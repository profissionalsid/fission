package canaryconfigmgr

import (
	"fmt"
	"time"
	"golang.org/x/net/context"

	promApi "github.com/prometheus/client_golang/api/prometheus"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

type PrometheusApiClient struct {
	client promApi.QueryAPI
	// Add more stuff later
}

// TODO  prometheusSvc will need to come from helm chart value and passed to controller pod.
// controllerpod then passes this during canaryConfigMgr create
func makePrometheusClient(prometheusSvc string) *PrometheusApiClient {
	promApiConfig := promApi.Config{
		Address: prometheusSvc,
	}

	promApiClient, err := promApi.New(promApiConfig)
	if err != nil {
		log.Errorf("Error creating prometheus api client for svc : %s, err : %v", prometheusSvc, err)
	}

	apiQueryClient := promApi.NewQueryAPI(promApiClient)

	return &PrometheusApiClient{
		client: apiQueryClient,
	}
}

func(promApi *PrometheusApiClient) GetFunctionFailurePercentage(funcName string, funcNs string, timeDuration time.Time) {
	queryString := fmt.Sprintf("fission_function_errors_total{name=%s,namespace=%s}", funcName, funcNs)
	val, err := promApi.client.Query(context.Background(), queryString, timeDuration)
	if err != nil {
		log.Errorf("Error querying prometheus for fission_function_errors_total, err : %v", err)
	}

	//jsonData, err := value.Type().MarshalJSON()
	//if err != nil {
	//	log.Printf("Error marshalling value into json. err : %v", err)
	//}
	//
	//json.Unmarshal(jsonData, model.Vector)


	switch {
	case val.Type() == model.ValScalar:
		scalarVal := val.(*model.Scalar)
		// handle scalar stuff
	case val.Type() == model.ValVector:
		vectorVal := val.(model.Vector)
		for _, elem := range vectorVal {
			log.Printf("labels : %s, Elem value : %v", elem.Metric, elem.Value)
			//TODO : Calculate here
		}
	default:
		log.Printf("type uncrecognized")
	}
}