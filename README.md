Azure Janitor
==============================

[![license](https://img.shields.io/github/license/webdevops/azure-janitor.svg)](https://github.com/webdevops/azure-janitor/blob/master/LICENSE)
[![DockerHub](https://img.shields.io/badge/DockerHub-webdevops%2Fazure--janitor-blue)](https://hub.docker.com/r/webdevops/azure-janitor/)
[![Quay.io](https://img.shields.io/badge/Quay.io-webdevops%2Fazure--janitor-blue)](https://quay.io/repository/webdevops/azure-janitor)

Janitor for Azure ResourceGroups and Resources based on ttl tag and Deployments based on TTL and limit.

Configuration
-------------

Normally no configuration is needed but can be customized using environment variables.

| Environment variable              | DefaultValue                | Description                                                       |
|-----------------------------------|-----------------------------|-------------------------------------------------------------------|
| `AZURE_SUBSCRIPTION_ID`           | `empty`                     | Azure Subscription IDs (empty for auto lookup)                    |
| `DRYRUN`                          | `empty`                     | DryRun (non deleting mode)                                        |
| `SERVER_BIND`                     | `:8080`                     | IP/Port binding for metrics and healthcheck                       |
| `JANITOR_INTERVAL`                | `1h`                        | How often Azure Janitor should cleanup the subscriptions (time.Duration) |
| `JANITOR_TAG`                     | `ttl`                       | Resource tag name for ttl value (non deleting mode)               |
| `JANITOR_RESOURCE_APIVERSION`     | `2019-03-01`                | API version for Azure Resource deletion                           |
| `JANITOR_DISABLE_RESOURCEGROUPS`  | `false`                     | Enable/Disable Azure ResourceGroup clearing                       |
| `JANITOR_DISABLE_RESOURCES`       | `false`                     | Enable/Disable Azure Resource clearing                            |
| `JANITOR_DISABLE_DEPLOYMENTS`     | `false`                     | Enable/Disable Azure Deployment clearing                          |
| `JANITOR_FILTER_RESOURCES`        | `empty`                     | Additional Azure REST API $filter for Azure ResourceGroups        |
| `JANITOR_FILTER_RESOURCEGROUPS`   | `empty`                     | Additional Azure REST API $filter for Azure Resources             |
| `JANITOR_DEPLOYMENT_TTL`          | `8760h`                     | TTL (Expiry) for Azure ResourceGroup Deployments                  |
| `JANITOR_DEPLOYMENT_LIMIT`        | `700`                       | Limit (count) of Azure ResourceGroup Deployments per ResourceGroup (Azure limit: 800) |

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
