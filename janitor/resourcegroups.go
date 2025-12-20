package janitor

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/webdevops/go-common/log/slogger"
	prometheusCommon "github.com/webdevops/go-common/prometheus"
	"github.com/webdevops/go-common/utils/to"
)

func (j *Janitor) runResourceGroups(ctx context.Context, logger *slogger.Logger, subscription *armsubscriptions.Subscription, filter string, callback chan<- func()) {
	contextLogger := logger.With(slog.String("task", "resourceGroup"))
	resourceType := "Microsoft.Resources/resourceGroups"

	client, err := armresources.NewResourceGroupsClient(*subscription.SubscriptionID, j.Azure.Client.GetCred(), j.Azure.Client.NewArmClientOptions())
	if err != nil {
		panic(err)
	}

	resourceTtl := prometheusCommon.NewMetricsList()

	pager := client.NewListPager(nil)
	for pager.More() {
		result, err := pager.NextPage(ctx)
		if err != nil {
			panic(err)
		}

		for _, resourceGroup := range result.Value {
			resourceLogger := contextLogger.With(slog.String("resource", to.String(resourceGroup.ID)))

			if resourceGroup.Tags != nil {
				resourceExpiryTime, resourceExpired, resourceTagUpdateNeeded := j.checkAzureResourceExpiry(resourceLogger, resourceType, *resourceGroup.ID, &resourceGroup.Tags)

				if resourceExpiryTime != nil {
					labels := prometheus.Labels{
						"subscriptionID": to.StringLower(subscription.SubscriptionID),
						"resourceID":     to.StringLower(resourceGroup.ID),
						"resourceGroup":  to.StringLower(resourceGroup.Name),
						"resourceType":   strings.ToLower(resourceType),
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
							"subscriptionID": to.StringLower(subscription.SubscriptionID),
							"resourceType":   strings.ToLower(resourceType),
						}).Inc()
					}
				}

				if !j.Conf.DryRun && resourceExpired {
					resourceLogger.Infof("expired, trying to delete")
					if _, err := client.BeginDelete(ctx, *resourceGroup.Name, nil); err == nil {
						// successfully deleted
						resourceLogger.Infof("successfully deleted")

						j.Prometheus.MetricDeletedResource.With(prometheus.Labels{
							"subscriptionID": to.StringLower(subscription.SubscriptionID),
							"resourceType":   strings.ToLower(resourceType),
						}).Inc()
					} else {
						// failed delete
						resourceLogger.Error(err.Error())

						j.Prometheus.MetricErrors.With(prometheus.Labels{
							"subscriptionID": to.StringLower(subscription.SubscriptionID),
							"resourceType":   strings.ToLower(resourceType),
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
