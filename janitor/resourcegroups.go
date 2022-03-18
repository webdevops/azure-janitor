package janitor

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/profiles/2020-09-01/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	prometheusCommon "github.com/webdevops/go-prometheus-common"
)

func (j *Janitor) runResourceGroups(ctx context.Context, subscription subscriptions.Subscription, filter string, ttlMetricsChan chan<- *prometheusCommon.MetricList) {
	contextLogger := log.WithField("task", "resourceGroup")
	resourceType := "Microsoft.Resources/resourceGroups"

	client := resources.NewGroupsClientWithBaseURI(j.Azure.Environment.ResourceManagerEndpoint, *subscription.SubscriptionID)
	j.decorateAzureAutorest(&client.Client)

	resourceTtl := prometheusCommon.NewMetricsList()

	resourceGroupResult, err := client.ListComplete(ctx, filter, nil)
	if err != nil {
		panic(err)
	}

	for _, resourceGroup := range *resourceGroupResult.Response().Value {
		resourceLogger := contextLogger.WithField("resource", *resourceGroup.ID)

		if resourceGroup.Tags != nil {
			resourceExpiryTime, resourceExpired, resourceTagUpdateNeeded := j.checkAzureResourceExpiry(resourceLogger, resourceType, *resourceGroup.ID, &resourceGroup.Tags)

			if resourceExpiryTime != nil {
				resourceTtl.AddTime(prometheus.Labels{
					"subscriptionID": *subscription.SubscriptionID,
					"resourceID":     *resourceGroup.ID,
					"resourceGroup":  *resourceGroup.Name,
					"provider":       resourceType,
				}, *resourceExpiryTime)
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
					resourceLogger.Errorf("ERROR %s", err)

					j.Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			}

			if !j.Conf.DryRun && resourceExpired {
				resourceLogger.Infof("expired, trying to delete")
				if _, err := client.Delete(ctx, *resourceGroup.Name); err == nil {
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

	ttlMetricsChan <- resourceTtl
}
