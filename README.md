# Azure Janitor

[![license](https://img.shields.io/github/license/webdevops/azure-janitor.svg)](https://github.com/webdevops/azure-janitor/blob/master/LICENSE)
[![DockerHub](https://img.shields.io/badge/DockerHub-webdevops%2Fazure--janitor-blue)](https://hub.docker.com/r/webdevops/azure-janitor/)
[![Quay.io](https://img.shields.io/badge/Quay.io-webdevops%2Fazure--janitor-blue)](https://quay.io/repository/webdevops/azure-janitor)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/azure-janitor)](https://artifacthub.io/packages/search?repo=azure-janitor)

Janitor for Azure

Janitor tasks:
- ResourceGroup cleanup based on TTL tag
- Resource cleanup based on TTL tag
- ResourceGroup Deployment cleanup based on TTL and limit (count)
- RoleAssignments cleanup based on RoleDefinitionIds and TTL

## Usage

```
Usage:
  azure-janitor [OPTIONS]

Application Options:
      --dry-run                                   Dry run (no delete) [$DRYRUN]
      --log.debug                                 debug mode [$LOG_DEBUG]
      --log.devel                                 development mode [$LOG_DEVEL]
      --log.json                                  Switch log output to json format [$LOG_JSON]
      --azure.environment=                        Azure environment name (default: AZUREPUBLICCLOUD) [$AZURE_ENVIRONMENT]
      --azure.subscription=                       Azure subscription ID (space delimiter) [$AZURE_SUBSCRIPTION_ID]
      --azure.resource-tag=                       Azure Resource tags (space delimiter) (default: owner) [$AZURE_RESOURCE_TAG]
      --janitor.interval=                         Janitor interval (time.duration) (default: 1h) [$JANITOR_INTERVAL]
      --janitor.tag=                              Janitor azure tag (string) (default: ttl) [$JANITOR_TAG]
      --janitor.tag.target=                       Janitor azure tag (string) (default: ttl_expiry) [$JANITOR_TAG_TARGET]
      --janitor.resourcegroups                    Enable Azure ResourceGroups cleanup [$JANITOR_RESOURCEGROUPS_ENABLE]
      --janitor.resourcegroups.filter=            Additional $filter for Azure REST API for ResourceGroups [$JANITOR_RESOURCEGROUPS_FILTER]
      --janitor.resources                         Enable Azure Resources cleanup [$JANITOR_RESOURCES_ENABLE]
      --janitor.resources.filter=                 Additional $filter for Azure REST API for Resources [$JANITOR_RESOURCES_FILTER]
      --janitor.deployments                       Enable Azure Deployments cleanup [$JANITOR_DEPLOYMENTS_ENABLE]
      --janitor.deployments.ttl=                  Janitor deployment ttl (time.duration) (default: 8760h) [$JANITOR_DEPLOYMENTS_TTL]
      --janitor.deployments.limit=                Janitor deployment limit count (int) (default: 700) [$JANITOR_DEPLOYMENTS_LIMIT]
      --janitor.roleassignments                   Enable Azure RoleAssignments cleanup [$JANITOR_ROLEASSIGNMENTS_ENABLE]
      --janitor.roleassignments.ttl=              Janitor roleassignment ttl (time.duration) (default: 6h) [$JANITOR_ROLEASSIGNMENTS_TTL]
      --janitor.roleassignments.roledefinitionid= Janitor roledefinition ID (eg:
                                                  /subscriptions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDef-
                                                  initions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx or
                                                  /providers/Microsoft.Authorization/roleDefinitions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx for
                                                  subscription independent roleDefinitions)  (space delimiter)
                                                  [$JANITOR_ROLEASSIGNMENTS_ROLEDEFINITIONID]
      --janitor.roleassignments.filter=           Additional $filter for Azure REST API for RoleAssignments
                                                  [$JANITOR_ROLEASSIGNMENTS_FILTER]
      --janitor.roleassignments.descriptionttl=   Regexp for detecting ttl inside description of RoleAssignment
                                                  [$JANITOR_ROLEASSIGNMENTS_DESCRIPTIONTTL]
      --server.bind=                              Server address (default: :8080) [$SERVER_BIND]
      --server.timeout.read=                      Server read timeout (default: 5s) [$SERVER_TIMEOUT_READ]
      --server.timeout.write=                     Server write timeout (default: 10s) [$SERVER_TIMEOUT_WRITE]

Help Options:
  -h, --help                                      Show this help message
```

for Azure API authentication (using ENV vars)
see https://docs.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication

For AzureCLI authentication set `AZURE_AUTH=az`

## Azure tag

By default the Azure Janitor is using `ttl` as tag and sets the expiry timestamp to `ttl_expiry`.
Based on timestamp in `ttl_expiry` it will trigger the cleanup of the corresponding resource if expired.

Both `ttl` and `ttl_expiry` name can be changed and could also be set to the same Azure tag.

Supported absolute timestamps

- 2006-01-02 15:04:05 +07:00
- 2006-01-02 15:04:05 MST
- 2006-01-02 15:04:05
- 02 Jan 06 15:04 MST (RFC822)
- 02 Jan 06 15:04 -0700 (RFC822Z)
- Monday, 02-Jan-06 15:04:05 MST (RFC850)
- Mon, 02 Jan 2006 15:04:05 MST (RFC1123)
- Mon, 02 Jan 2006 15:04:05 -0700 (RFC1123Z)
- 2006-01-02T15:04:05Z07:00 (RFC3339)
- 2006-01-02T15:04:05.999999999Z07:00 (RFC3339Nano)
- 2006-01-02


Supported relative timestamps
(tag will be updated with absolute timestamp as soon it's found)

- [ISO8601 (durations)](https://en.wikipedia.org/wiki/ISO_8601#Durations)
    - PT24H (24h)
    - P1M (1 month)
- [golang (extended) durations](https://github.com/karrick/tparse)
    - 1h (1 hour)
    - 1d (1 day)
    - 1w (1 week)
    - 1mo (1 month)
    - 1y (1 year)

## RoleAssignments

**General RoleAssignment TTL**

To cleanup Azure RoleAssignments a list of Azure RoleDefinitions (multiple possible) have to be set for security reasons:

```
/azure-janitor \
    --janitor.roleassignments \
    --janitor.roleassignments.roledefinitionid=/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx \
    --janitor.roleassignments.roledefinitionid=/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx \
    --janitor.roleassignments.roledefinitionid=/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx \
    --janitor.roleassignments.ttl=6h
```

This can be used for cleanup of temporary RoleAssignments.
Expiry time is calculated based on Azure RoleAssignment creation time and specified TTL.

**RoleAssignment based TTL**

As RoleAssignments only have a `description` a custom (must less then the default ttl) ttl can be specified
with a custom format and can be parsed with RegExp:

```
/azure-janitor \
    --janitor.roleassignments \
    --janitor.roleassignments.roledefinitionid=/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx \
    --janitor.roleassignments.roledefinitionid=/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx \
    --janitor.roleassignments.roledefinitionid=/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx \
    --janitor.roleassignments.ttl=6h \
    --janitor.roleassignments.descriptionttl='\[ttl:([^\]]+)\]'
```

RoleAssignment example with ttl in description:
```
    {
      "properties": {
        "roleDefinitionId": "/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx",
        "principalId": "xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx",
        "principalType": "User",
        "scope": "/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/resourceGroups/XXXXXXXXXXXXXX/providers/...",
        "condition": null,
        "conditionVersion": null,
        "createdOn": "2021-03-29T19:41:18.9035423Z",
        "updatedOn": "2021-03-29T19:41:18.9035423Z",
        "createdBy": "xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx",
        "updatedBy": "xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx",
        "delegatedManagedIdentityResourceId": null,
        "description": "[ttl:1h]"
      },
      "id": "/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/resourceGroups/XXXXXXXXXXXXXX/providers/.../providers/Microsoft.Authorization/roleAssignments/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx",
      "type": "Microsoft.Authorization/roleAssignments",
      "name": "xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx"
    },
```

## ARM template usage

Using relative time (duration):
```
{
  "$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
  "contentVersion": "1.0.0.0",
  "parameters": {
    // ...
  },
  "variables": {
    "tags": {
      "ttl": "1mo"
    }
  },
  "resources": [
    // ...
    {
      "name": "foobar",
      "type": "Microsoft.KeyVault/vaults",
      "apiVersion": "2018-02-14",
      "location": "westeurope",
      "tags": "[variables('tags')]",
      "properties": {
        // ...
      }
    }
    // ...
  ],
  "outputs": {
    "tags": {
      "value": "[variables('tags')]",
      "type": "object"
    }
  }
}
```

Using absolute calculated time:
```
{
  "$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
  "contentVersion": "1.0.0.0",
  "parameters": {
    "baseTime": {
      "type": "string",
      "defaultValue": "[utcNow('u')]"
    }
  },
  "variables": {
    "tags": {
      "ttl": "[dateTimeAdd(parameters('baseTime'), 'P1M')]"
    }
  },
  "resources": [
    // ...
    {
      "name": "foobar",
      "type": "Microsoft.KeyVault/vaults",
      "apiVersion": "2018-02-14",
      "location": "westeurope",
      "tags": "[variables('tags')]",
      "properties": {
        // ...
      }
    }
    // ...
  ],
  "outputs": {
    "tags": {
      "value": "[variables('tags')]",
      "type": "object"
    }
  }
}

```

## Metrics

| Metric                                 | Type         | Description                                                                              |
|----------------------------------------|--------------|------------------------------------------------------------------------------------------|
| `azurejanitor_duration`                | Gauge        | Duration of cleanup run in seconds                                                       |
| `azurejanitor_deployment`              | Gauge        | Count of deployment based on scope (empty ``resourceGroup`` label == subscription scope) |
| `azurejanitor_resource_ttl`            | Gauge        | List of Azure Resources and ResourceGroups with labels and expiry timestamp as value     |
| `azurejanitor_roleassignment_ttl`      | Gauge        | List of Azure RoleAssignments with expiry timestamp as value                             |
| `azurejanitor_resources_deleted_count` | Counter      | Number of deleted resources (by resource type)                                           |
| `azurejanitor_error_count`             | Counter      | Number of failed deleted resources (by resource type)                                    |

### ResourceTags handling

see [armclient tagmanager documentation](https://github.com/webdevops/go-common/blob/main/azuresdk/README.md#tag-manager)

### AzureTracing metrics

see [armclient tracing documentation](https://github.com/webdevops/go-common/blob/main/azuresdk/README.md#azuretracing-metrics)
