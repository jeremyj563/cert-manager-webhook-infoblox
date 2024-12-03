# Infoblox solver testing

If you are developing and want to test the webhook, you will need to create files containing the config and credentials with access to your Infoblox instance.  
The files needs to be in the directory specified in the `main_test.go` file (the directory this readme is in).

Due to these files possibly containing sensitive information, all JSON and YAML files in this directory is excluded from being version controlled.

## Config

The config needs to be supplied in JSON format, so create a file and name it something memorable like `config.json` and enter the following.  

```json
{
    "host": "IB_ADDRESS",
    "view": "IB_VIEW",
    "usernameSecretRef": {
        "name": "infoblox-credentials",
        "key": "username"
    },
    "passwordSecretRef": {
        "name": "infoblox-credentials",
        "key": "password"
    }
}
```

Replace the variables `IB_ADDRESS` and `IB_VIEW` with the address of the Infoblox server, and the view that DNS01 challenges can use to resolve.

## Credentials

The credentials needs to be supplied in a YAML format, so simirarly to the config, create a file and name it something memorable like `credentials.yaml` and enter the following.  

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: infoblox-credentials
type: Opaque
data:
  username: dXNlcm5hbWUK # base64 encoded string "username"
  password: cGFzc3dvcmQK # base64 encoded string "password"
```

The username and password supplied needs to be base64 encoded.
