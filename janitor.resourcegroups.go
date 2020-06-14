package main

import (
	"context"
	"github.com/Azure/azure-sdk-for-go/profiles/2019-03-01/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/google/logger"
	"github.com/prometheus/client_golang/prometheus"
	prometheusCommon "github.com/webdevops/go-prometheus-common"
)


func (j *Janitor) runResourceGroups(ctx context.Context, subscription subscriptions.Subscription, filter string, ttlMetricsChan chan<- *prometheusCommon.MetricList) {
	client := resources.NewGroupsClient(*subscription.SubscriptionID)
	client.Authorizer = AzureAuthorizer

	resourceTtl := prometheusCommon.NewMetricsList()

	resourceGroupResult, err := client.ListComplete(ctx, filter, nil)
	if err != nil {
		panic(err)
	}

	for _, resourceGroup := range *resourceGroupResult.Response().Value {
		// resourceGroup.Type is nil
		resourceType := "Microsoft.Resources/resourceGroups"

		if resourceGroup.Tags != nil {
			resourceExpiryTime, resourceExpired, resourceTagUpdateNeeded := j.checkAzureResourceExpiry(resourceType, *resourceGroup.ID, &resourceGroup.Tags)

			if resourceExpiryTime != nil {
				resourceTtl.AddTime(prometheus.Labels{
					"subscriptionID": *subscription.SubscriptionID,
					"resourceID":     *resourceGroup.ID,
					"resourceGroup":  *resourceGroup.Name,
					"provider":       resourceType,
				}, *resourceExpiryTime)
			}

			if !opts.DryRun && resourceTagUpdateNeeded {
				logger.Infof("%s: tag update needed, updating resource", *resourceGroup.ID)
				resourceGroupOpts := resources.GroupPatchable{
					Tags: resourceGroup.Tags,
				}

				if _, err := client.Update(ctx, *resourceGroup.Name, resourceGroupOpts); err == nil {
					// successfully deleted
					logger.Infof("%s: successfully updated", *resourceGroup.ID)
				} else {
					// failed delete
					logger.Errorf("%s: ERROR %s", *resourceGroup.ID, err)

					Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			}

			if !opts.DryRun && resourceExpired {
				logger.Infof("%s: expired, trying to delete", *resourceGroup.ID)
				if _, err := client.Delete(ctx, *resourceGroup.Name); err == nil {
					// successfully deleted
					logger.Infof("%s: successfully deleted", *resourceGroup.ID)

					Prometheus.MetricDeletedResource.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				} else {
					// failed delete
					logger.Errorf("%s: ERROR %s", *resourceGroup.ID, err)

					Prometheus.MetricErrors.With(prometheus.Labels{
						"resourceType": resourceType,
					}).Inc()
				}
			}
		}
	}

	ttlMetricsChan <- resourceTtl
}
