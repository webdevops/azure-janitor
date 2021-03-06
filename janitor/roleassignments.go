package janitor

import (
	"context"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/Azure/azure-sdk-for-go/services/preview/authorization/mgmt/2020-04-01-preview/authorization"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	prometheusCommon "github.com/webdevops/go-prometheus-common"
	"time"
)

func (j *Janitor) runRoleAssignments(ctx context.Context, subscription subscriptions.Subscription, filter string, ttlMetricsChan chan<- *prometheusCommon.MetricList) {
	contextLogger := log.WithField("task", "roleAssignment")

	resourceTtl := prometheusCommon.NewMetricsList()
	resourceType := "Microsoft.Authorization/roleAssignments"

	client := authorization.NewRoleAssignmentsClientWithBaseURI(j.Azure.Environment.ResourceManagerEndpoint, *subscription.SubscriptionID)
	client.Authorizer = j.Azure.Authorizer

	result, err := client.ListComplete(ctx, filter)
	if err != nil {
		panic(err)
	}

	for _, roleAssignment := range *result.Response().Value {
		if roleAssignment.RoleDefinitionID == nil || roleAssignment.CreatedOn == nil {
			continue
		}

		roleAssignmentLogger := contextLogger.WithFields(log.Fields{
			"roleAssignmentId": *roleAssignment.ID,
			"scope":            *roleAssignment.Scope,
			"principalId":      *roleAssignment.PrincipalID,
			"principalType":    string(roleAssignment.PrincipalType),
			"roleDefinitionId": *roleAssignment.RoleDefinitionID,
			"subscriptionID":   *subscription.SubscriptionID,
			"resourceGroup":    extractResourceGroupFromAzureId(*roleAssignment.Scope),
		})

		// check if RoleDefinitionID is set
		// do not want to touch other RoleAssignments
		if stringInSlice(*roleAssignment.RoleDefinitionID, j.Conf.Janitor.RoleAssignments.RoleDefintionIds) {
			var roleAssignmentTtl *time.Duration
			if j.Conf.Logger.Verbose {
				roleAssignmentLogger.Infof("checking ttl")
			}

			// detect ttl from description
			if j.Conf.Janitor.RoleAssignments.DescriptionTtlRegExp != nil && roleAssignment.Description != nil {
				descriptionTtlMatch := j.Conf.Janitor.RoleAssignments.DescriptionTtlRegExp.FindSubmatch([]byte(*roleAssignment.Description))

				if len(descriptionTtlMatch) >= 2 {
					if v, err := j.parseExpiryDuration(string(descriptionTtlMatch[1])); err == nil {
						roleAssignmentTtl = v
					}
				}
			}

			// use default ttl if no ttl was detected or ttl is higher then default
			if roleAssignmentTtl == nil || roleAssignmentTtl.Seconds() > j.Conf.Janitor.RoleAssignments.Ttl.Seconds() {
				roleAssignmentTtl = &j.Conf.Janitor.RoleAssignments.Ttl
			}

			// calulate expiry and check if already expired
			roleAssignmentExpiry := roleAssignment.CreatedOn.UTC().Add(*roleAssignmentTtl)
			roleAssignmentExpired := time.Now().After(roleAssignmentExpiry)

			roleAssignmentLogger.Debugf("detected ttl %v", roleAssignmentTtl.String())

			resourceTtl.AddTime(prometheus.Labels{
				"roleAssignmentId": *roleAssignment.ID,
				"scope":            *roleAssignment.Scope,
				"principalId":      *roleAssignment.PrincipalID,
				"principalType":    string(roleAssignment.PrincipalType),
				"roleDefinitionId": *roleAssignment.RoleDefinitionID,
				"subscriptionID":   *subscription.SubscriptionID,
				"resourceGroup":    extractResourceGroupFromAzureId(*roleAssignment.Scope),
			}, roleAssignmentExpiry)

			if roleAssignmentExpired {
				if !j.Conf.DryRun {
					roleAssignmentLogger.Infof("expired, trying to delete")
					if _, err := client.DeleteByID(ctx, *roleAssignment.ID); err == nil {
						// successfully deleted
						roleAssignmentLogger.Infof("successfully deleted")

						j.Prometheus.MetricDeletedResource.With(prometheus.Labels{
							"subscriptionID": *subscription.SubscriptionID,
							"resourceType":   resourceType,
						}).Inc()
					} else {
						// failed delete
						roleAssignmentLogger.Errorf("ERROR %s", err)

						j.Prometheus.MetricErrors.With(prometheus.Labels{
							"subscriptionID": *subscription.SubscriptionID,
							"resourceType":   resourceType,
						}).Inc()
					}
				} else {
					roleAssignmentLogger.Infof("expired, but dryrun active")
				}
			} else {
				if j.Conf.Logger.Verbose {
					roleAssignmentLogger.Infof("NOT expired")
				}
			}
		}
	}

	ttlMetricsChan <- resourceTtl
}
