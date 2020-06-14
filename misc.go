package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	resourceGroupFromResourceIdRegExp = regexp.MustCompile("/resourceGroups/([^/]*)")
	providerFromResourceIdRegExp      = regexp.MustCompile("/providers/([^/]*)")
)

func extractResourceGroupFromAzureId(azureId string) (resourceGroup string) {
	if subMatch := resourceGroupFromResourceIdRegExp.FindStringSubmatch(azureId); len(subMatch) >= 1 {
		resourceGroup = subMatch[1]
	}

	return
}

func extractProviderFromAzureId(azureId string) (provider string) {
	if subMatch := providerFromResourceIdRegExp.FindStringSubmatch(azureId); len(subMatch) >= 1 {
		provider = subMatch[1]
	}

	return
}

func timeToFloat64(v time.Time) float64 {
	return float64(v.Unix())
}
