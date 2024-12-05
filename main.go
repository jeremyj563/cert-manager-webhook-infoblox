package main

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"

	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	ibclient "github.com/infobloxopen/infoblox-go-client/v2"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	logf "github.com/cert-manager/cert-manager/pkg/logs"
)

var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		logf.V(logf.ErrorLevel).ErrorS(nil, "No value specified for environment variable 'GROUP_NAME'")
		panic("GROUP_NAME must be specified")
	}

	// This will register our custom DNS provider with the webhook serving
	// library, making it available as an API under the provided GroupName.
	// You can register multiple DNS provider implementations with a single
	// webhook, where the Name() method will be used to disambiguate between
	// the different implementations.
	cmd.RunWebhookServer(GroupName,
		&solver{},
	)
}

// solver implements the provider-specific logic needed to
// 'present' an ACME challenge TXT record for your own DNS provider.
// To do so, it must implement the `github.com/cert-manager/cert-manager/pkg/acme/webhook.Solver`
// interface.
type solver struct {
	// If a Kubernetes 'clientset' is needed, you must:
	// 1. uncomment the additional `client` field in this structure below
	// 2. uncomment the "k8s.io/client-go/kubernetes" import at the top of the file
	// 3. uncomment the relevant code in the Initialize method below
	// 4. ensure your webhook's service account has the required RBAC role
	//    assigned to it for interacting with the Kubernetes APIs you need.
	client kubernetes.Clientset
}

// providerConfig is a structure that is used to decode into when
// solving a DNS01 challenge.
// This information is provided by cert-manager, and may be a reference to
// additional configuration that's needed to solve the challenge for this
// particular certificate or issuer.
// This typically includes references to Secret resources containing DNS
// provider credentials, in cases where a 'multi-tenant' DNS solver is being
// created.
// If you do *not* require per-issuer or per-certificate configuration to be
// provided to your webhook, you can skip decoding altogether in favour of
// using CLI flags or similar to provide configuration.
// You should not include sensitive information here. If credentials need to
// be used by your provider here, you should reference a Kubernetes Secret
// resource and fetch these credentials using a Kubernetes clientset.
type providerConfig struct {
	// Change the two fields below according to the format of the configuration
	// to be decoded.
	// These fields will be set by users in the
	// `issuer.spec.acme.dns01.providers.webhook.config` field.

	View              string                   `json:"view"`
	Host              string                   `json:"host"`
	Scheme            string                   `json:"scheme" default:"https"`
	Port              string                   `json:"port" default:"443"`
	Version           string                   `json:"version" default:"2.8"`
	SslVerify         bool                     `json:"sslVerify" default:"true"`
	UsernameSecretRef cmmeta.SecretKeySelector `json:"usernameSecretRef"`
	PasswordSecretRef cmmeta.SecretKeySelector `json:"passwordSecretRef"`
}

// Name is used as the name for this DNS solver when referencing it on the ACME
// Issuer resource.
// This should be unique **within the group name**, i.e. you can have two
// solvers configured with the same Name() **so long as they do not co-exist
// within a single webhook deployment**.
// For example, `cloudflare` may be used as the name of a solver.
func (s *solver) Name() string {
	return "infoblox"
}

// Present is responsible for actually presenting the DNS record with the
// DNS provider.
// This method should tolerate being called multiple times with the same value.
// cert-manager itself will later perform a self check to ensure that the
// solver has correctly configured the DNS provider.
func (s *solver) Present(ch *v1alpha1.ChallengeRequest) error {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return err
	}

	// Initialize a new Infoblox client
	c, err := s.newInfobloxClient(&cfg, ch.ResourceNamespace)
	if err != nil {
		return err
	}

	// Check if TXT record already exist
	name := strings.TrimSuffix(ch.ResolvedFQDN, ".")

	reference, err := s.checkForRecord(c, cfg.View, name, ch.Key)

	// Create a record in the DNS provider's console if none exist
	if reference != "" {
		logf.V(logf.InfoLevel).InfoS("Skipping creation, existing record found", "name", name, "reference", reference)
	} else {
		reference, err := s.createTXTRecord(c, name, ch.Key, cfg.View)
		if err != nil {
			return err
		}

		logf.V(logf.InfoLevel).InfoS("Created TXT record", "name", name, "reference", reference)
	}

	return nil
}

// CleanUp should delete the relevant TXT record from the DNS provider console.
// If multiple TXT records exist with the same record name (e.g.
// _acme-challenge.example.com) then **only** the record with the same `key`
// value provided on the ChallengeRequest should be cleaned up.
// This is in order to facilitate multiple DNS validations for the same domain
// concurrently.
func (s *solver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return err
	}

	// Initialize a new Infoblox client
	c, err := s.newInfobloxClient(&cfg, ch.ResourceNamespace)
	if err != nil {
		return err
	}

	// Check if TXT record already exist
	name := strings.TrimSuffix(ch.ResolvedFQDN, ".")

	reference, err := s.checkForRecord(c, cfg.View, name, ch.Key)
	if err != nil {
		logf.V(logf.InfoLevel).InfoS("There was an error when checking if record already exist", "name", name, "reference", reference)
	}

	// Delete the record in the DNS provider's console if it is found
	if reference == "" {
		logf.V(logf.InfoLevel).InfoS("Skipping deletion, no existing record found", "name", name, "reference", reference)
	} else {
		_, err := s.deleteTXTRecord(c, reference)
		if err != nil {
			return err
		}

		logf.V(logf.InfoLevel).InfoS("Deleted TXT record", "name", name, "reference", reference)
	}

	return nil
}

// Initialize will be called when the webhook first starts.
// This method can be used to instantiate the webhook, i.e. initialising
// connections or warming up caches.
// Typically, the kubeClientConfig parameter is used to build a Kubernetes
// client that can be used to fetch resources from the Kubernetes API, e.g.
// Secret resources containing credentials used to authenticate with DNS
// provider accounts.
// The stopCh can be used to handle early termination of the webhook, in cases
// where a SIGTERM or similar signal is sent to the webhook process.
func (s *solver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	///// UNCOMMENT THE BELOW CODE TO MAKE A KUBERNETES CLIENTSET AVAILABLE TO
	///// YOUR CUSTOM DNS PROVIDER

	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}

	s.client = *cl

	///// END OF CODE TO MAKE KUBERNETES CLIENTSET AVAILABLE
	return nil
}

// loadConfig is a small helper function that decodes JSON configuration into
// the typed config struct.
func loadConfig(cfgJSON *extapi.JSON) (providerConfig, error) {
	cfg := providerConfig{}
	// handle the 'base case' where no configuration has been provided
	if cfgJSON == nil {
		return cfg, nil
	}
	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		logf.V(logf.ErrorLevel).ErrorS(nil, "There was an error decoding the solver config")
		return cfg, err
	}

	return cfg, nil
}

///// BEGINNING OF CODE FOR INFOBLOX SOLVER

func (s *solver) newInfobloxClient(cfg *providerConfig, namespace string) (ibclient.IBConnector, error) {
	username, err := s.getSecret(cfg.UsernameSecretRef, namespace)
	if err != nil {
		return nil, err
	}

	password, err := s.getSecret(cfg.PasswordSecretRef, namespace)
	if err != nil {
		return nil, err
	}

	_t := reflect.TypeOf(providerConfig{})
	if cfg.Port == "" {
		_f, _ := _t.FieldByName("Port")
		cfg.Port = _f.Tag.Get("default")
	}

	if cfg.Version == "" {
		_f, _ := _t.FieldByName("Version")
		cfg.Version = _f.Tag.Get("default")
	}

	hostConfig := ibclient.HostConfig{
		Scheme:  "https",
		Host:    cfg.Host,
		Version: cfg.Version,
		Port:    cfg.Port,
	}

	authConfig := ibclient.AuthConfig{
		Username: username,
		Password: password,
	}

	transportConfig := ibclient.NewTransportConfig(strconv.FormatBool(cfg.SslVerify), 60, 10)
	requestBuilder := &ibclient.WapiRequestBuilder{}
	requestor := &ibclient.WapiHttpRequestor{}

	c, err := ibclient.NewConnector(hostConfig, authConfig, transportConfig, requestBuilder, requestor)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (s *solver) getSecret(sel cmmeta.SecretKeySelector, namespace string) (string, error) {
	secret, err := s.client.CoreV1().Secrets(namespace).Get(context.Background(), sel.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	secretData, ok := secret.Data[sel.Key]
	if !ok {
		return "", err
	}

	return strings.TrimSuffix(string(secretData), "\n"), nil
}

func (s *solver) checkForRecord(c ibclient.IBConnector, view string, name string, text string) (string, error) {
	var records []ibclient.RecordTXT
	record := ibclient.NewRecordTXT("", "", "", "", uint32(0), false, "", ibclient.EA{})
	params := map[string]string{
		"name": name,
		"text": text,
		"view": view,
	}
	err := c.GetObject(record, "", ibclient.NewQueryParams(false, params), &records)

	if len(records) > 0 {
		return records[0].Ref, err
	} else {
		return "", err
	}
}

func (s *solver) createTXTRecord(c ibclient.IBConnector, name string, text string, view string) (string, error) {
	return c.CreateObject(ibclient.NewRecordTXT(view, "", name, text, uint32(0), false, "", ibclient.EA{}))
}

func (s *solver) deleteTXTRecord(c ibclient.IBConnector, ref string) (string, error) {
	return c.DeleteObject(ref)
}
