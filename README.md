Azure Janitor
==============================

[![license](https://img.shields.io/github/license/webdevops/azure-janitor.svg)](https://github.com/webdevops/azure-janitor/blob/master/LICENSE)
[![DockerHub](https://img.shields.io/badge/DockerHub-webdevops%2Fazure--janitor-blue)](https://hub.docker.com/r/webdevops/azure-janitor/)
[![Quay.io](https://img.shields.io/badge/Quay.io-webdevops%2Fazure--janitor-blue)](https://quay.io/repository/webdevops/azure-janitor)

Janitor for Azure ResourceGroups and Resources based on ttl tag and Deployments based on TTL and limit.

Usage
-----

```
Usage:
  azure-janitor [OPTIONS]

Application Options:
      --dry-run                         Dry run (no delete) [$DRYRUN]
      --debug                           debug mode [$DEBUG]
  -v, --verbose                         verbose mode [$VERBOSE]
      --log.json                        Switch log output to json format [$LOG_JSON]
      --azure-environment=              Azure environment name (default: AZUREPUBLICCLOUD) [$AZURE_ENVIRONMENT]
      --azure-subscription=             Azure subscription ID [$AZURE_SUBSCRIPTION_ID]
      --janitor.interval=               Janitor interval (time.duration) (default: 1h) [$JANITOR_INTERVAL]
      --janitor.tag=                    Janitor azure tag (string) (default: ttl) [$JANITOR_TAG]
      --janitor.deployment.ttl=         Janitor deploument ttl (time.duration) (default: 8760h)
                                        [$JANITOR_DEPLOYMENT_TTL]
      --janitor.deployment.limit=       Janitor deploument limit count (int) (default: 700) [$JANITOR_DEPLOYMENT_LIMIT]
      --janitor.disable.resourcegroups  Disable Azure ResourceGroups cleanup [$JANITOR_DISABLE_RESOURCEGROUPS]
      --janitor.disable.resources       Disable Azure Resources cleanup [$JANITOR_DISABLE_RESOURCES]
      --janitor.disable.deployments     Disable Azure Deployments cleanup [$JANITOR_DISABLE_DEPLOYMENTS]
      --janitor.filter.resourcegroups=  Additional $filter for Azure REST API for ResourceGroups
                                        [$JANITOR_FILTER_RESOURCEGROUPS]
      --janitor.filter.resources=       Additional $filter for Azure REST API for Resources [$JANITOR_FILTER_RESOURCES]
      --bind=                           Server address (default: :8080) [$SERVER_BIND]

Help Options:
  -h, --help                            Show this help message
```

for Azure API authentication (using ENV vars) see https://github.com/Azure/azure-sdk-for-go#authentication

Azure tag
-------------

By default the Azure Janitor is using `ttl` as tag to trigger a cleanup if the resource is expired.

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

Metrics
-------

| Metric                                         | Type         | Description                                                                           |
|------------------------------------------------|--------------|---------------------------------------------------------------------------------------|
| `azurejanitor_duration`                        | Gauge        | Duration of cleanup run in seconds                                                    |
| `azurejanitor_resources_ttl`                   | Gauge        | List of Azure resources and resourcegroups with labels and expiry timestamp as value  |
| `azurejanitor_resources_deleted`               | Counter      | Number of deleted resources (by resource type)                                        |
| `azurejanitor_errors`                          | Counter      | Number of failed deleted resources (by resource type)                                 |
