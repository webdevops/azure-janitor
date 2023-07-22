package janitor

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	"github.com/prometheus/client_golang/prometheus"
	prometheusCommon "github.com/webdevops/go-common/prometheus"
	"github.com/webdevops/go-common/utils/to"
	"go.uber.org/zap"
)

func (j *Janitor) runResourceGroups(ctx context.Context, logger *zap.SugaredLogger, subscription *armsubscriptions.Subscription, filter string, callback chan<- func()) {
	contextLogger := logger.With(zap.String("task", "resourceGroup"))
	resourceType := "Microsoft.Resources/resourceGroups"

	client, err := armresources.NewResourceGroupsClient(*subscription.SubscriptionID, j.Azure.Client.GetCred(), j.Azure.Client.NewArmClientOptions())
	if err != nil {
		logger.Panic(err)
	}

	resourceTtl := prometheusCommon.NewMetricsList()

	pager := client.NewListPager(nil)
	for pager.More() {
		result, err := pager.NextPage(ctx)
		if err != nil {
			logger.Panic(err)
		}

		for _, resourceGroup := range result.Value {
			resourceLogger := contextLogger.With(zap.String("resource", to.String(resourceGroup.ID)))

			if resourceGroup.Tags != nil {
				resourceExpiryTime, resourceExpired, resourceTagUpdateNeeded := j.checkAzureResourceExpiry(resourceLogger, resourceType, *resourceGroup.ID, &resourceGroup.Tags)

				if resourceExpiryTime != nil {
					labels := prometheus.Labels{
						"subscriptionID": stringPtrToStringLower(subscription.SubscriptionID),
						"resourceID":     stringPtrToStringLower(resourceGroup.ID),
						"resourceGroup":  stringPtrToStringLower(resourceGroup.Name),
						"resourceType":   stringToStringLower(resourceType),
					}
					labels = j.Azure.ResourceTagManager.AddResourceTagsToPrometheusLabels(ctx, labels, *resourceGroup.ID)
					resourceTtl.AddTime(labels, *resourceExpiryTime)
				}

				if !j.Conf.DryRun && resourceTagUpdateNeeded {
					resourceLogger.Infof("tag update needed, updating resource")
					resourceGroupOpts := armresources.ResourceGroupPatchable{
						Tags: resourceGroup.Tags,
					}

					if _, err := client.Update(ctx, *resourceGroup.Name, resourceGroupOpts, nil); err == nil {
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
					if _, err := client.BeginDelete(ctx, *resourceGroup.Name, nil); err == nil {
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
	}

	callback <- func() {
		resourceTtl.GaugeSet(j.Prometheus.MetricTtlResources)
	}
}
