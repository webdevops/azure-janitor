package janitor

import (
	"context"
	"time"

	"github.com/Azure/azure-sdk-for-go/profiles/2020-09-01/resources/mgmt/resources"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	prometheusCommon "github.com/webdevops/go-common/prometheus"
)

func (j *Janitor) runDeployments(ctx context.Context, subscription subscriptions.Subscription, ttlMetricsChan chan<- *prometheusCommon.MetricList) {
	var deploymentCounter, deploymentFinalCounter int64
	contextLogger := log.WithField("task", "deployment")

	client := resources.NewGroupsClientWithBaseURI(j.Azure.Environment.ResourceManagerEndpoint, *subscription.SubscriptionID)
	j.decorateAzureAutorest(&client.Client)

	resourceTtl := prometheusCommon.NewMetricsList()

	deploymentClient := resources.NewDeploymentsClientWithBaseURI(j.Azure.Environment.ResourceManagerEndpoint, *subscription.SubscriptionID)
	j.decorateAzureAutorest(&deploymentClient.Client)

	resourceType := "Microsoft.Resources/deployments"

	resourceGroupResult, err := client.ListComplete(ctx, "", nil)
	if err != nil {
		panic(err)
	}

	for _, resourceGroup := range *resourceGroupResult.Response().Value {
		deploymentCounter = 0
		deploymentFinalCounter = 0

		resourceLogger := contextLogger.WithField("resource", to.String(resourceGroup.ID))

		deploymentResult, err := deploymentClient.ListByResourceGroupComplete(ctx, *resourceGroup.Name, "", nil)
		if err != nil {
			panic(err)
		}

		for _, deployment := range *deploymentResult.Response().Value {
			deleteDeployment := false
			deploymentCounter++

			if deploymentCounter >= j.Conf.Janitor.Deployments.Limit {
				// limit reached
				deleteDeployment = true
			} else if deployment.Properties != nil && deployment.Properties.Timestamp != nil {
				// expire check
				deploymentAge := time.Since(deployment.Properties.Timestamp.Time)
				if deploymentAge.Seconds() > j.Conf.Janitor.Deployments.Ttl.Seconds() {
					deleteDeployment = true
				}
			}

			if !j.Conf.DryRun && deleteDeployment {
				if _, err := deploymentClient.Delete(ctx, to.String(resourceGroup.Name), to.String(deployment.Name)); err == nil {
					// successfully deleted
					resourceLogger.Infof("%s: successfully deleted", to.String(deployment.ID))

					j.Prometheus.MetricDeletedResource.With(prometheus.Labels{
						"subscriptionID": stringPtrToStringLower(subscription.SubscriptionID),
						"resourceType":   stringToStringLower(resourceType),
					}).Inc()
				} else {
					// failed delete
					resourceLogger.Errorf("%s: ERROR %s", to.String(deployment.ID), err)

					j.Prometheus.MetricErrors.With(prometheus.Labels{
						"subscriptionID": stringPtrToStringLower(subscription.SubscriptionID),
						"resourceType":   stringToStringLower(resourceType),
					}).Inc()
				}
			} else {
				deploymentFinalCounter++
			}
		}

		resourceLogger.Infof("found %v deployments, %v still existing, %v deleted", deploymentCounter, deploymentFinalCounter, deploymentCounter-deploymentFinalCounter)
	}

	ttlMetricsChan <- resourceTtl
}
