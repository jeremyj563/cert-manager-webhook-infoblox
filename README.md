# ACME webhook for Infoblox

Forked from the [cert-manager/webhook-example](https://github.com/cert-manager/webhook-example) repository.  
Heavily inspired from the work done by [Luis Gracia](https://github.com/luisico) on their now archived  [cert-manager-webhook-infoblox-wapi](https://github.com/luisico/cert-manager-webhook-infoblox-wapi) project.

## Adding the repo

Add the helm repo using the following command:

```bash
helm repo add cert-manager-webhook-infoblox https://tazthemaniac.github.io/cert-manager-webhook-infoblox/
```

## Installing

Begin by installing cert-manager following the official instructions.  
<https://cert-manager.io/docs/installation/>

After that, install the chart in the cert-manager namespace, and remember to set the `groupName` to a unique value.

```bash
helm install cert-manager-webhook-infoblox --namespace cert-manager \
  cert-manager-webhook-infoblox/cert-manager-webhook-infoblox \
  --set groupName="acme.department.company.com"
```

Full list of settings and default values at the bottom.

## Setting up an issuer

For use with ingress in multiple namespaces a `ClusterIssuer` issuer is recommended.  
The cluster issuer is also the part that takes in the Infoblox settings, like username, password, and view.

In total, four resources needs to be created for a cluster issuer to work.

* The Infoblox credentials secret
* A role that is allowed to read the secret
* A role binding for the service account that this chart creates
* And lastly, the issuer itself.

The `issuer-sample.yaml` file in this repo can be used as a starting point, but don't forget to customize it before applying it.

### Issuer

Lets start with the issuer itself, it is the thing that will need the most customization.  
Take the following template for letsencryp's staging and customize it by replacing the following variables with the ones you need.

* `YOUR_EMAIL` The email that should be associated with the certificates.
* `GROUP_NAME` The group name you specified when you installed the chart.
* `INFOBLOX_ADDRESS` The address or FQDN of the Infoblox server.
* `INFOBLOX_VIEW` The name of the Infoblox view that can present TXT records for a DNS01 challenge.

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-staging
spec:
  acme:
    email: YOUR_EMAIL
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: letsencrypt-account-key
    solvers:
    - dns01:
        webhook:
          groupName: GROUP_NAME
          solverName: infoblox
          config:
            host: INFOBLOX_ADDRESS
            view: INFOBLOX_VIEW
            usernameSecretRef:
              name: infoblox-credentials
              key: username
            passwordSecretRef:
              name: infoblox-credentials
              key: password
```

The `usernameSecretRef` and `passwordSecretRef` name fields refer to the name of the secret that contains the username and password to access your Infoblox setup.

### Credentials

The credentials are a simple opaque secret that contains the username and password for Infoblox.  
Remember to encode the username and password using base64 when creating the secret using kubectl.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: infoblox-credentials
  namespace: cert-manager
type: Opaque
data:
  username: dXNlcm5hbWUK # base64 encoded string "username"
  password: cGFzc3dvcmQK # base64 encoded string "password"
```

### Role and RoleBinding

Lastly there needs to be a way for the service account to read the secret containing the Infoblox username and password. This is done by creating a role with the correct permissions, and a role binding that connects the service account and role.

Take note of the name you gave the secret in the last step and set the same name in the `resourceNames` part of the role bellow.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: webhook-infoblox:secret-reader
  namespace: cert-manager
rules:
  - apiGroups: [""]
    resources:
      - secrets
    resourceNames:
      - infoblox-credentials
    verbs:
      - get
      - watch

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: webhook-infoblox:secret-reader
  namespace: cert-manager
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: webhook-infoblox:secret-reader
subjects:
  - apiGroup: ""
    kind: ServiceAccount
    name: webhook-infoblox
    namespace: cert-manager
```

## Settings and default values

Following is a list of all the options available when installing the chart and the default values.

| Name                           | Default Value                              |
| ------------------------------ | ------------------------------------------ |
| groupName                      | ""                                         |
| nameOverride                   | ""                                         |
| fullNameOverride               | ""                                         |
| rootCACertificate.duration     | 43800h                                     |
| servingCertificate.duration    | 8760h                                      |
| certManager.namespace          | cert-manager                               |
| certManager.serviceAccountName | cert-manager                               |
| image.repository               | tazthemaniac/cert-manager-webhook-infoblox |
| image.tag                      | latest                                     |
| image.pullPolicy               | IfNotPresent                               |
| service.type                   | ClusterIP                                  |
| service.port                   | 443                                        |
| resources                      | {}                                         |
| nodeSelector                   | {}                                         |
| tolerations                    | []                                         |
| affinity                       | {}                                         |
