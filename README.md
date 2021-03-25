Azure Janitor
==============================

[![license](https://img.shields.io/github/license/webdevops/azure-janitor.svg)](https://github.com/webdevops/azure-janitor/blob/master/LICENSE)
[![DockerHub](https://img.shields.io/badge/DockerHub-webdevops%2Fazure--janitor-blue)](https://hub.docker.com/r/webdevops/azure-janitor/)
[![Quay.io](https://img.shields.io/badge/Quay.io-webdevops%2Fazure--janitor-blue)](https://quay.io/repository/webdevops/azure-janitor)

Janitor for Azure

Janitor tasks:
- ResourceGroup cleanup based on TTL tag
- Resource cleanup based on TTL tag
- ResourceGroup Deployment cleanup based on TTL and limit (count)
- RoleAssignments cleanup based on RoleDefinitionIds and TTL

Usage
-----

```
Usage:
  azure-janitor [OPTIONS]

Application Options:
      --dry-run                                   Dry run (no delete) [$DRYRUN]
      --debug                                     debug mode [$DEBUG]
  -v, --verbose                                   verbose mode [$VERBOSE]
      --log.json                                  Switch log output to json format [$LOG_JSON]
      --azure-environment=                        Azure environment name (default: AZUREPUBLICCLOUD) [$AZURE_ENVIRONMENT]
      --azure-subscription=                       Azure subscription ID [$AZURE_SUBSCRIPTION_ID]
      --janitor.interval=                         Janitor interval (time.duration) (default: 1h) [$JANITOR_INTERVAL]
      --janitor.tag=                              Janitor azure tag (string) (default: ttl) [$JANITOR_TAG]
      --janitor.tag.target=                       Janitor azure tag (string) (default: ttl_expiry) [$JANITOR_TAG_TARGET]
      --janitor.resourcegroups.enable             Enable Azure ResourceGroups cleanup [$JANITOR_RESOURCEGROUPS_ENABLE]
      --janitor.resourcegroups.filter=            Additional $filter for Azure REST API for ResourceGroups
                                                  [$JANITOR_RESOURCEGROUPS_FILTER]
      --janitor.resources.enable                  Enable Azure Resources cleanup [$JANITOR_RESOURCES_ENABLE]
      --janitor.resources.filter=                 Additional $filter for Azure REST API for Resources
                                                  [$JANITOR_RESOURCES_FILTER]
      --janitor.deployments                       Enable Azure Deployments cleanup [$JANITOR_DEPLOYMENTS_ENABLE]
      --janitor.deployments.ttl=                  Janitor deployment ttl (time.duration) (default: 8760h)
                                                  [$JANITOR_DEPLOYMENTS_TTL]
      --janitor.deployments.limit=                Janitor deployment limit count (int) (default: 700)
                                                  [$JANITOR_DEPLOYMENTS_LIMIT]
      --janitor.roleassignments.enable            Enable Azure RoleAssignments cleanup [$JANITOR_ROLEASSIGNMENTS_ENABLE]
      --janitor.roleassignments.ttl=              Janitor roleassignment ttl (time.duration) (default: 6h)
                                                  [$JANITOR_ROLEASSIGNMENTS_TTL]
      --janitor.roleassignments.roledefinitionid= Janitor roledefinition ID (eg:
                                                  /subscriptions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Author-

                                                  ization/roleDefinitions/xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx)
                                                  [$JANITOR_ROLEASSIGNMENTS_ROLEDEFINITIONID]
      --janitor.roleassignments.filter=           Additional $filter for Azure REST API for RoleAssignments
                                                  [$JANITOR_ROLEASSIGNMENTS_FILTER]
      --bind=                                     Server address (default: :8080) [$SERVER_BIND]

Help Options:
  -h, --help                                      Show this help message
```

for Azure API authentication (using ENV vars) see https://github.com/Azure/azure-sdk-for-go#authentication

Azure tag
---------

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

RoleAssignments
---------------

To cleanup Azure RoleAssignments a list of Azure RoleDefinitions (multiple possible) have to be set for security reasons:

```
/azure-janitor \
    --janitor.roleassignments.enable \
    --janitor.roleassignments.roledefinitionid=/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx \
    --janitor.roleassignments.roledefinitionid=/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx \
    --janitor.roleassignments.roledefinitionid=/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx/providers/Microsoft.Authorization/roleDefinitions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx \
    --janitor.roleassignments.ttl=6h
```

This can be used for cleanup of temporary RoleAssignments.
Expiry time is calculated based on Azure RoleAssignment creation time and specified TTL.

Metrics
-------

| Metric                                         | Type         | Description                                                                           |
|------------------------------------------------|--------------|---------------------------------------------------------------------------------------|
| `azurejanitor_duration`                        | Gauge        | Duration of cleanup run in seconds                                                    |
| `azurejanitor_resources_ttl`                   | Gauge        | List of Azure Resources and ResourceGroups with labels and expiry timestamp as value  |
| `azurejanitor_roleassignment_ttl`              | Gauge        | List of Azure RoleAssignments with expiry timestamp as value                          |
| `azurejanitor_resources_deleted`               | Counter      | Number of deleted resources (by resource type)                                        |
| `azurejanitor_errors`                          | Counter      | Number of failed deleted resources (by resource type)                                 |
