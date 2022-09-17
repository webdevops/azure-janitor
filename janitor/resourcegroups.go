package janitor

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/profiles/2020-09-01/resources/mgmt/resources" //nolint:staticcheck
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions" //nolint:staticcheck
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	azureCommon "github.com/webdevops/go-common/azure"
	prometheusCommon "github.com/webdevops/go-common/prometheus"
)

func (j *Janitor) runResourceGroups(ctx context.Context, logger *log.Entry, subscription subscriptions.Subscription, filter string, ttlMetricsChan chan<- *prometheusCommon.MetricList) {
	contextLogger := logger.WithField("task", "resourceGroup")
	resourceType := "Microsoft.Resources/resourceGroups"

	client := resources.NewGroupsClientWithBaseURI(j.Azure.Client.Environment.ResourceManagerEndpoint, *subscription.SubscriptionID)
	j.decorateAzureAutorest(&client.Client)

	resourceTtl := prometheusCommon.NewMetricsList()

	resourceGroupResult, err := client.ListComplete(ctx, filter, nil)
	if err != nil {
		panic(err.Error())
	}

	for _, resourceGroup := range *resourceGroupResult.Response().Value {
		resourceLogger := contextLogger.WithField("resource", to.String(resourceGroup.ID))

		if resourceGroup.Tags != nil {
			resourceExpiryTime, resourceExpired, resourceTagUpdateNeeded := j.checkAzureResourceExpiry(resourceLogger, resourceType, *resourceGroup.ID, &resourceGroup.Tags)

			if resourceExpiryTime != nil {
				labels := prometheus.Labels{
					"subscriptionID": stringPtrToStringLower(subscription.SubscriptionID),
					"resourceID":     stringPtrToStringLower(resourceGroup.ID),
					"resourceGroup":  stringPtrToStringLower(resourceGroup.Name),
					"resourceType":   stringToStringLower(resourceType),
				}
				labels = azureCommon.AddResourceTagsToPrometheusLabels(labels, resourceGroup.Tags, j.Conf.Azure.ResourceTags)
				resourceTtl.AddTime(labels, *resourceExpiryTime)
			}

			if !j.Conf.DryRun && resourceTagUpdateNeeded {
				resourceLogger.Infof("tag update needed, updating resource")
				resourceGroupOpts := resources.GroupPatchable{
					Tags: resourceGroup.Tags,
				}

				if _, err := client.Update(ctx, *resourceGroup.Name, resourceGroupOpts); err == nil {
					// successfully deleted
					resourceLogger.Infof("successfully updated")
				} else {
					// failed delete
					resourceLogger.Error(err.Error())

					j.Prometheus.MetricErrors.With(prometheus.Labels{
						"subscriptionID": stringPtrToStringLower(subscription.SubscriptionID),
						"resourceType":   stringToStringLower(resourceType),
					}).Inc()
				}
			}

			if !j.Conf.DryRun && resourceExpired {
				resourceLogger.Infof("expired, trying to delete")
				if _, err := client.Delete(ctx, *resourceGroup.Name); err == nil {
					// successfully deleted
					resourceLogger.Infof("successfully deleted")

					j.Prometheus.MetricDeletedResource.With(prometheus.Labels{
						"subscriptionID": stringPtrToStringLower(subscription.SubscriptionID),
						"resourceType":   stringToStringLower(resourceType),
					}).Inc()
				} else {
					// failed delete
					resourceLogger.Error(err.Error())

					j.Prometheus.MetricErrors.With(prometheus.Labels{
						"subscriptionID": stringPtrToStringLower(subscription.SubscriptionID),
						"resourceType":   stringToStringLower(resourceType),
					}).Inc()
				}
			}
		}
	}

	ttlMetricsChan <- resourceTtl
}
