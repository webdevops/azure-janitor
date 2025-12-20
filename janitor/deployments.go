package janitor

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/webdevops/go-common/log/slogger"
	prometheusCommon "github.com/webdevops/go-common/prometheus"
	"github.com/webdevops/go-common/utils/to"
)

func (j *Janitor) runDeployments(ctx context.Context, logger *slogger.Logger, subscription *armsubscriptions.Subscription, callback chan<- func()) {
	var deploymentCounter, deploymentFinalCounter int64
	contextLogger := logger.With(slog.String("task", "deployment"))

	client, err := armresources.NewResourceGroupsClient(*subscription.SubscriptionID, j.Azure.Client.GetCred(), j.Azure.Client.NewArmClientOptions())
	if err != nil {
		panic(err)
	}

	deploymentMetric := prometheusCommon.NewMetricsList()

	deploymentClient, err := armresources.NewDeploymentsClient(*subscription.SubscriptionID, j.Azure.Client.GetCred(), j.Azure.Client.NewArmClientOptions())
	if err != nil {
		panic(err)
	}

	resourceType := "Microsoft.Resources/deployments"

	// -------------------------------------
	// Subscription deployments
	deploymentPager := deploymentClient.NewListAtSubscriptionScopePager(nil)
	if err != nil {
		panic(err)
	}

	deploymentCounter = 0
	deploymentFinalCounter = 0
	for deploymentPager.More() {
		deploymentResult, err := deploymentPager.NextPage(ctx)
		if err != nil {
			panic(err)
		}

		for _, deployment := range deploymentResult.Value {
			deleteDeployment := false
			deploymentCounter++

			if deploymentCounter >= j.Conf.Janitor.Deployments.Limit {
				// limit reached
				deleteDeployment = true
			} else if deployment.Properties != nil && deployment.Properties.Timestamp != nil {
				// expire check
				deploymentAge := time.Since(deployment.Properties.Timestamp.UTC())
				if deploymentAge.Seconds() > j.Conf.Janitor.Deployments.Ttl.Seconds() {
					deleteDeployment = true
				}
			}

			if !j.Conf.DryRun && deleteDeployment {
				if _, err := deploymentClient.BeginDeleteAtSubscriptionScope(ctx, to.String(deployment.Name), nil); err == nil {
					// successfully deleted
					contextLogger.Infof("%s: successfully deleted", to.String(deployment.ID))

					j.Prometheus.MetricDeletedResource.With(prometheus.Labels{
						"subscriptionID": to.StringLower(subscription.SubscriptionID),
						"resourceType":   strings.ToLower(resourceType),
					}).Inc()
				} else {
					// failed delete
					contextLogger.Errorf("%s: ERROR %s", to.String(deployment.ID), err.Error())

					j.Prometheus.MetricErrors.With(prometheus.Labels{
						"subscriptionID": to.StringLower(subscription.SubscriptionID),
						"resourceType":   strings.ToLower(resourceType),
					}).Inc()
				}
			} else {
				deploymentFinalCounter++
			}
		}
	}

	deploymentMetric.Add(prometheus.Labels{
		"subscriptionID": to.String(subscription.SubscriptionID),
		"resourceGroup":  "",
	}, float64(deploymentFinalCounter))

	contextLogger.Infof("found %v deployments on Subscription scope, %v still existing, %v deleted", deploymentCounter, deploymentFinalCounter, deploymentCounter-deploymentFinalCounter)

	// -------------------------------------
	// ResourceGroup deployments
	resourceGroupPager := client.NewListPager(nil)
	for resourceGroupPager.More() {
		resourceGroupResult, err := resourceGroupPager.NextPage(ctx)
		if err != nil {
			panic(err)
		}

		for _, resourceGroup := range resourceGroupResult.Value {
			deploymentCounter = 0
			deploymentFinalCounter = 0

			resourceLogger := contextLogger.With(slog.String("resource", to.String(resourceGroup.ID)))

			deploymentPager := deploymentClient.NewListByResourceGroupPager(*resourceGroup.Name, nil)
			if err != nil {
				panic(err)
			}

			for deploymentPager.More() {
				deploymentResult, err := deploymentPager.NextPage(ctx)
				if err != nil {
					panic(err)
				}

				for _, deployment := range deploymentResult.Value {
					deleteDeployment := false
					deploymentCounter++

					if deploymentCounter >= j.Conf.Janitor.Deployments.Limit {
						// limit reached
						deleteDeployment = true
					} else if deployment.Properties != nil && deployment.Properties.Timestamp != nil {
						// expire check
						deploymentAge := time.Since(deployment.Properties.Timestamp.UTC())
						if deploymentAge.Seconds() > j.Conf.Janitor.Deployments.Ttl.Seconds() {
							deleteDeployment = true
						}
					}

					if !j.Conf.DryRun && deleteDeployment {
						if _, err := deploymentClient.BeginDelete(ctx, to.String(resourceGroup.Name), to.String(deployment.Name), nil); err == nil {
							// successfully deleted
							resourceLogger.Infof("%s: successfully deleted", to.String(deployment.ID))

							j.Prometheus.MetricDeletedResource.With(prometheus.Labels{
								"subscriptionID": to.StringLower(subscription.SubscriptionID),
								"resourceType":   strings.ToLower(resourceType),
							}).Inc()
						} else {
							// failed delete
							resourceLogger.Errorf("%s: ERROR %s", to.String(deployment.ID), err.Error())

							j.Prometheus.MetricErrors.With(prometheus.Labels{
								"subscriptionID": to.StringLower(subscription.SubscriptionID),
								"resourceType":   strings.ToLower(resourceType),
							}).Inc()
						}
					} else {
						deploymentFinalCounter++
					}
				}
			}

			deploymentMetric.Add(prometheus.Labels{
				"subscriptionID": to.String(subscription.SubscriptionID),
				"resourceGroup":  to.String(resourceGroup.Name),
			}, float64(deploymentFinalCounter))

			resourceLogger.Infof("found %v deployments on ResourceGroup scope, %v still existing, %v deleted", deploymentCounter, deploymentFinalCounter, deploymentCounter-deploymentFinalCounter)
		}
	}

	callback <- func() {
		deploymentMetric.GaugeSet(j.Prometheus.MetricDeployment)
	}
}
