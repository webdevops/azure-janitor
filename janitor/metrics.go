package janitor

import (
	"github.com/prometheus/client_golang/prometheus"
)

func (j *Janitor) initPrometheus() {
	var err error
	j.Azure.ResourceTagManager, err = j.Azure.Client.TagManager.ParseTagConfig(j.Conf.Azure.ResourceTags)
	if err != nil {
		j.Logger.Fatal(`unable to parse resourceTag configuration "%s": %v"`, j.Conf.Azure.ResourceTags, err.Error())
	}

	j.Prometheus.MetricDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurejanitor_duration",
			Help: "AzureJanitor cleanup duration",
		},
		[]string{},
	)
	prometheus.MustRegister(j.Prometheus.MetricDuration)

	j.Prometheus.MetricDeployment = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurejanitor_deployment",
			Help: "AzureJanitor count of deployments on scope",
		},
		[]string{
			"subscriptionID",
			"resourceGroup",
		},
	)
	prometheus.MustRegister(j.Prometheus.MetricDeployment)

	j.Prometheus.MetricTtlResources = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurejanitor_resource_ttl",
			Help: "AzureJanitor resources with expiry time",
		},
		j.Azure.ResourceTagManager.AddToPrometheusLabels(
			[]string{
				"resourceID",
				"subscriptionID",
				"resourceGroup",
				"resourceType",
			},
		),
	)
	prometheus.MustRegister(j.Prometheus.MetricTtlResources)

	j.Prometheus.MetricTtlRoleAssignments = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurejanitor_roleassignment_ttl",
			Help: "AzureJanitor roleassignments with expiry time",
		},
		[]string{
			"roleAssignmentId",
			"scope",
			"principalId",
			"principalType",
			"roleDefinitionId",
			"subscriptionID",
			"resourceGroup",
		},
	)
	prometheus.MustRegister(j.Prometheus.MetricTtlRoleAssignments)

	j.Prometheus.MetricDeletedResource = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "azurejanitor_resource_deleted_count",
			Help: "AzureJanitor deleted resources",
		},
		[]string{
			"subscriptionID",
			"resourceType",
		},
	)
	prometheus.MustRegister(j.Prometheus.MetricDeletedResource)

	j.Prometheus.MetricErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "azurejanitor_error_count",
			Help: "AzureJanitor error counter",
		},
		[]string{
			"subscriptionID",
			"resourceType",
		},
	)
	prometheus.MustRegister(j.Prometheus.MetricErrors)
}
