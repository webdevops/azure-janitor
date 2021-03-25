package main

import (
	"context"
	"github.com/Azure/azure-sdk-for-go/profiles/2020-09-01/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	prometheusCommon "github.com/webdevops/go-prometheus-common"
)

func (j *Janitor) runResources(ctx context.Context, subscription subscriptions.Subscription, filter string, ttlMetricsChan chan<- *prometheusCommon.MetricList) {
	contextLogger := log.WithField("task", "resource")

	client := resources.NewClientWithBaseURI(azureEnvironment.ResourceManagerEndpoint, *subscription.SubscriptionID)
	client.Authorizer = azureAuthorizer

	resourceTtl := prometheusCommon.NewMetricsList()

	resourceResult, err := client.ListComplete(ctx, filter, "", nil)
	if err != nil {
		panic(err)
	}

	for _, resource := range *resourceResult.Response().Value {
		resourceType := *resource.Type
		resourceTypeApiVersion := j.getAzureApiVersionForSubscriptionResourceType(*subscription.SubscriptionID, resourceType)

		resourceLogger := contextLogger.WithField("resource", *resource.ID)

		if resourceTypeApiVersion != "" && resource.Tags != nil {
			resourceExpiryTime, resourceExpired, resourceTagUpdateNeeded := j.checkAzureResourceExpiry(resourceLogger, resourceType, *resource.ID, &resource.Tags)

			if resourceExpiryTime != nil {
				resourceTtl.AddTime(prometheus.Labels{
					"subscriptionID": *subscription.SubscriptionID,
					"resourceID":     *resource.ID,
					"resourceGroup":  extractResourceGroupFromAzureId(*resource.ID),
					"provider":       extractProviderFromAzureId(*resource.ID),
				}, *resourceExpiryTime)
			}

			if !opts.DryRun && resourceTagUpdateNeeded {
				resourceLogger.Infof("tag update needed, updating resource")
				resourceOpts := resources.GenericResource{
					Name: resource.Name,
					Tags: resource.Tags,
				}

				if _, err := client.UpdateByID(ctx, *resource.ID, resourceTypeApiVersion, resourceOpts); err == nil {
					// successfully deleted
					resourceLogger.Infof("successfully updated")
				} else {
					// failed delete
					resourceLogger.Errorf("ERROR %s", err)

					Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			}

			if !opts.DryRun && resourceExpired {
				resourceLogger.Infof("expired, trying to delete")
				if _, err := client.DeleteByID(ctx, *resource.ID, resourceTypeApiVersion); err == nil {
					// successfully deleted
					resourceLogger.Infof("successfully deleted")

					Prometheus.MetricDeletedResource.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				} else {
					// failed delete
					resourceLogger.Errorf("ERROR %s", err)

					Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			}
		}
	}

	ttlMetricsChan <- resourceTtl
}
