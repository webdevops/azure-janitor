package janitor

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/webdevops/go-common/azuresdk/armclient"
	prometheusCommon "github.com/webdevops/go-common/prometheus"
	"github.com/webdevops/go-common/utils/to"
	"go.uber.org/zap"
)

func (j *Janitor) runResources(ctx context.Context, logger *zap.SugaredLogger, subscription *armsubscriptions.Subscription, filter string, ttlMetricsChan chan<- *prometheusCommon.MetricList) {
	contextLogger := logger.With(zap.String("task", "resource"))

	client, err := armresources.NewClient(*subscription.SubscriptionID, j.Azure.Client.GetCred(), j.Azure.Client.NewArmClientOptions())
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

		for _, resource := range result.Value {
			resourceType := *resource.Type
			resourceTypeApiVersion := j.getAzureApiVersionForResourceType(*subscription.SubscriptionID, to.String(resource.Location), resourceType)

			resourceLogger := contextLogger.With(
				zap.String("resource", to.String(resource.ID)),
				zap.String("location", to.String(resource.Location)),
				zap.String("apiVersion", resourceTypeApiVersion),
			)

			if resourceTypeApiVersion == "" {
				resourceLogger.Errorf("unable to detect apiVersion for Azure resource, cannot delete resource (please report this issue as bug)")

				j.Prometheus.MetricErrors.With(prometheus.Labels{
					"subscriptionID": stringPtrToStringLower(subscription.SubscriptionID),
					"resourceType":   stringToStringLower(resourceType),
				}).Inc()

				continue
			}

			if resourceTypeApiVersion != "" && resource.Tags != nil {
				resourceExpiryTime, resourceExpired, resourceTagUpdateNeeded := j.checkAzureResourceExpiry(resourceLogger, resourceType, *resource.ID, &resource.Tags)

				azureResource, _ := armclient.ParseResourceId(*resource.ID)

				if resourceExpiryTime != nil {
					labels := prometheus.Labels{
						"subscriptionID": stringPtrToStringLower(subscription.SubscriptionID),
						"resourceID":     stringPtrToStringLower(resource.ID),
						"resourceGroup":  azureResource.ResourceGroup,
						"resourceType":   azureResource.ResourceType,
					}
					labels = j.Azure.ResourceTagManager.AddResourceTagsToPrometheusLabels(ctx, labels, *resource.ID)
					resourceTtl.AddTime(labels, *resourceExpiryTime)
				}

				if !j.Conf.DryRun && resourceTagUpdateNeeded {
					resourceLogger.Infof("tag update needed, updating resource")
					resourceOpts := armresources.GenericResource{
						Name: resource.Name,
						Tags: resource.Tags,
					}

					if _, err := client.BeginUpdateByID(ctx, *resource.ID, resourceTypeApiVersion, resourceOpts, nil); err == nil {
						// successfully deleted
						resourceLogger.Infof("successfully updated")
					} else {
						// failed delete
						resourceLogger.Errorf("ERROR %s", err)

						j.Prometheus.MetricErrors.With(prometheus.Labels{
							"subscriptionID": *subscription.SubscriptionID,
							"resourceType":   resourceType,
						}).Inc()
					}
				}

				if !j.Conf.DryRun && resourceExpired {
					resourceLogger.Infof("expired, trying to delete")
					if _, err := client.BeginDeleteByID(ctx, *resource.ID, resourceTypeApiVersion, nil); err == nil {
						// successfully deleted
						resourceLogger.Infof("successfully deleted")

						j.Prometheus.MetricDeletedResource.With(prometheus.Labels{
							"subscriptionID": *subscription.SubscriptionID,
							"resourceType":   resourceType,
						}).Inc()
					} else {
						// failed delete
						resourceLogger.Errorf("ERROR %s", err)

						j.Prometheus.MetricErrors.With(prometheus.Labels{
							"subscriptionID": *subscription.SubscriptionID,
							"resourceType":   resourceType,
						}).Inc()
					}
				}
			}
		}
	}

	ttlMetricsChan <- resourceTtl
}
