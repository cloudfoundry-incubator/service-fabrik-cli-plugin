package restore

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/SAP/service-fabrik-cli-plugin/constants"
	"github.com/SAP/service-fabrik-cli-plugin/errors"
	"github.com/SAP/service-fabrik-cli-plugin/guidTranslator"
	"github.com/SAP/service-fabrik-cli-plugin/helper"
	"github.com/cloudfoundry/cli/plugin"
	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type RestoreCommand struct {
	cliConnection plugin.CliConnection
}

func NewRestoreCommand(cliConnection plugin.CliConnection) *RestoreCommand {
	command := new(RestoreCommand)
	command.cliConnection = cliConnection
	return command
}

const (
	red   color.Attribute = color.FgRed
	green color.Attribute = color.FgGreen
	cyan  color.Attribute = color.FgCyan
	white color.Attribute = color.FgWhite
)

func AddColor(text string, textColor color.Attribute) string {
	printer := color.New(textColor).Add(color.Bold).SprintFunc()
	return printer(text)
}

type Configuration struct {
	ServiceBroker       string
	ServiceBrokerExtUrl string
	SkipSslFlag         bool
}

func GetBrokerName() string {
	return getConfiguration().ServiceBroker
}

func GetExtUrl() string {
	return getConfiguration().ServiceBrokerExtUrl
}

func GetskipSslFlag() bool {
	return getConfiguration().SkipSslFlag
}

func getConfiguration() Configuration {
	var path string
	var CF_HOME string = os.Getenv("CF_HOME")
	if CF_HOME == "" {
		CF_HOME = helper.GetHomeDir()
	}
	path = CF_HOME + "/.cf/conf.json"

	file, _ := os.Open(path)
	decoder := json.NewDecoder(file)
	configuration := Configuration{}
	err := decoder.Decode(&configuration)
	if err != nil {
		fmt.Println("error:", err)
	}
	return configuration
}

func GetHttpClient() *http.Client {
	//Skip ssl verification.
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: GetskipSslFlag()},
			Proxy:           http.ProxyFromEnvironment,
		},
		Timeout: time.Duration(180) * time.Second,
	}
	return client
}

func GetResponse(client *http.Client, req *http.Request) *http.Response {
	req.Header.Set("Authorization", helper.GetAccessToken(helper.ReadConfigJsonFile()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	errors.ErrorIsNil(err)
	return resp
}

func (c *RestoreCommand) StartRestore(cliConnection plugin.CliConnection, serviceInstanceName string, backupId string, timeStamp string, isGuidOperation bool) {
	fmt.Println("Starting restore for ", AddColor(serviceInstanceName, cyan), "...")

	if helper.GetAccessToken(helper.ReadConfigJsonFile()) == "" {
		errors.NoAccessTokenError("Access Token")
	}
	var userSpaceGuid string = helper.GetSpaceGUID(helper.ReadConfigJsonFile())
	client := GetHttpClient()
	var req_body = bytes.NewBuffer([]byte(""))
	if isGuidOperation == true {
		var jsonPrep string = `{"backup_guid": "` + backupId + `"}`
		var jsonStr = []byte(jsonPrep)
		req_body = bytes.NewBuffer(jsonStr)
	} else {
		parsedTimestamp, err := time.Parse(time.RFC3339, timeStamp)
		if err != nil {
			fmt.Println(AddColor("FAILED", red))
			fmt.Println(err)
			fmt.Println("Please enter time in ISO8061 format, example - 2018-11-12T11:45:26.371Z, 2018-11-12T11:45:26Z")
			return
		}
		var epochTime string = strconv.FormatInt(parsedTimestamp.UnixNano()/1000000, 10)
		var jsonprep string = `{"time_stamp": "` + epochTime + `", "space_guid": "` + userSpaceGuid + `"}`
		var jsonStr = []byte(jsonprep)
		req_body = bytes.NewBuffer(jsonStr)
	}
	fmt.Println(req_body)
	var guid string = guidTranslator.FindInstanceGuid(cliConnection, serviceInstanceName, nil, "")

	var apiEndpoint string = helper.GetApiEndpoint(helper.ReadConfigJsonFile())
	var broker string = GetBrokerName()
	var extUrl string = GetExtUrl()

	apiEndpoint = strings.Replace(apiEndpoint, "api", broker, 1)

	var url string = apiEndpoint + extUrl + "/service_instances/" + guid + "/restore"
	req, err := http.NewRequest("POST", url, req_body)
	var resp *http.Response = GetResponse(client, req)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	var respObject map[string]interface{}

	if err := json.Unmarshal(body, &respObject); err != nil {
		fmt.Println(err)
	}

	if respStatus, flag := respObject["status"].(float64); flag != false {
		if respStatus != 202 {
			fmt.Println(AddColor("FAILED", red))
			if respError, flag := respObject["error"].(string); flag != false {
				fmt.Println("Error: ", respError)
			}
			if respMessage, flag := respObject["description"].(string); flag != false {
				fmt.Println("Message: ", respMessage)
			}
		}
	}

	if resp.Status == constants.AcceptedHttpStatusResponse {
		fmt.Println(AddColor("OK", green))
		if respOperation, flag := respObject["name"].(string); flag != false {
			fmt.Println("Operation: ", respOperation)
		}
		if restoreGuid, flag := respObject["guid"].(string); flag != false {
			fmt.Println("Restore Guid: ", restoreGuid)
		}
		if isGuidOperation == true {
			fmt.Println("Restore has been initiated for the instance name:", AddColor(serviceInstanceName, cyan), " and from the backup id:", AddColor(backupId, cyan))
		} else {
			fmt.Println("Restore has been initiated for the instance name:", AddColor(serviceInstanceName, cyan), " using time stamp:", AddColor(timeStamp, cyan))
		}
		fmt.Println("Please check the status of restore by entering 'cf restore SERVICE_INSTANCE_NAME'")
	}

	errors.ErrorIsNil(err)

}

func (c *RestoreCommand) RestoreInfo(cliConnection plugin.CliConnection, serviceInstanceName string) {
	fmt.Println("Showing the status of the last restore operation for", AddColor(serviceInstanceName, cyan), " ...")

	if helper.GetAccessToken(helper.ReadConfigJsonFile()) == "" {
		errors.NoAccessTokenError("Access Token")
	}

	client := GetHttpClient()

	var guid string = guidTranslator.FindInstanceGuid(cliConnection, serviceInstanceName, nil, "")

	var userSpaceGuid string = helper.GetSpaceGUID(helper.ReadConfigJsonFile())

	var apiEndpoint string = helper.GetApiEndpoint(helper.ReadConfigJsonFile())
	var broker string = GetBrokerName()
	var extUrl string = GetExtUrl()

	apiEndpoint = strings.Replace(apiEndpoint, "api", broker, 1)

	var url string = apiEndpoint + extUrl + "/service_instances/" + guid + "/restore?space_guid=" + userSpaceGuid

	req, err := http.NewRequest("GET", url, nil)

	var resp *http.Response = GetResponse(client, req)

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	var respObject map[string]interface{}

	if err := json.Unmarshal(body, &respObject); err != nil {
		fmt.Println(err)
	}

	if respStatus, flag := respObject["status"].(float64); flag != false {
		if respStatus != 200 {
			fmt.Println(AddColor("FAILED", red))
			if respError, flag := respObject["error"].(string); flag != false {
				fmt.Println("Error: ", respError)
			}
			if respMessage, flag := respObject["description"].(string); flag != false {
				fmt.Println("Message: ", respMessage)
			}
		}
	}

	if resp.Status == constants.OKHttpStatusResponse {
		fmt.Println(AddColor("OK", green))

		table := tablewriter.NewWriter(os.Stdout)
		table.SetBorder(false)
		table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
		table.SetAlignment(tablewriter.ALIGN_LEFT)
		table.SetCenterSeparator(" ")
		table.SetColumnSeparator(" ")
		table.SetRowSeparator(" ")
		table.SetHeaderLine(false)
		table.SetAutoFormatHeaders(false)
		table.SetHeader([]string{" ", " "})

		if field, flag := respObject["service_id"].(string); flag != false {
			table.Append([]string{"service-name", strings.Trim(guidTranslator.FindServiceName(cliConnection, field, nil), "\"")})
		}

		if field, flag := respObject["plan_id"].(string); flag != false {
			table.Append([]string{"plan-name", strings.Trim(guidTranslator.FindPlanName(cliConnection, field, nil), "\"")})
		}

		if field, flag := respObject["instance_guid"].(string); flag != false {
			table.Append([]string{"instance-name", strings.Trim(guidTranslator.FindInstanceName(cliConnection, field, nil), "\"")})
		}

		table.Append([]string{"organization-name", helper.GetOrgName(helper.ReadConfigJsonFile())})
		table.Append([]string{"space-name", helper.GetSpaceName(helper.ReadConfigJsonFile())})

		if field, flag := respObject["username"].(string); flag != false {
			table.Append([]string{"username", field})
		}

		if field, flag := respObject["operation"].(string); flag != false {
			table.Append([]string{"operation", field})
		}

		if field, flag := respObject["backup_guid"].(string); flag != false {
			table.Append([]string{"backup_guid", field})
		}

		if field, flag := respObject["trigger"].(string); flag != false {
			table.Append([]string{"trigger", field})
		}

		if field, flag := respObject["state"].(string); flag != false {
			table.Append([]string{"state", field})
		}

		if field, flag := respObject["started_at"].(string); flag != false {
			table.Append([]string{"trigger", field})
		}

		if _, flag := respObject["finished_at"].(string); flag {
			table.Append([]string{"finished_at", respObject["finished_at"].(string)})
		} else {
			table.Append([]string{"finished_at", "null"})
		}
		table.Render()
	}
	errors.ErrorIsNil(err)
}

func (c *RestoreCommand) AbortRestore(cliConnection plugin.CliConnection, serviceInstanceName string) {
	fmt.Println("Aborting restore for ", AddColor(serviceInstanceName, cyan), "...")

	if helper.GetAccessToken(helper.ReadConfigJsonFile()) == "" {
		errors.NoAccessTokenError("Access Token")
	}

	client := GetHttpClient()

	var guid string = guidTranslator.FindInstanceGuid(cliConnection, serviceInstanceName, nil, "")

	var userSpaceGuid string = helper.GetSpaceGUID(helper.ReadConfigJsonFile())

	var apiEndpoint string = helper.GetApiEndpoint(helper.ReadConfigJsonFile())
	var broker string = GetBrokerName()
	var extUrl string = GetExtUrl()

	apiEndpoint = strings.Replace(apiEndpoint, "api", broker, 1)

	var url string = apiEndpoint + extUrl + "/service_instances/" + guid + "/restore?space_guid=" + userSpaceGuid
	req, err := http.NewRequest("DELETE", url, nil)

	var resp *http.Response = GetResponse(client, req)

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	var respObject map[string]interface{}
	if err := json.Unmarshal(body, &respObject); err != nil {
		fmt.Println(err)
	}

	if respStatus, flag := respObject["status"].(float64); flag != false {

		if (respStatus != 202) && (respStatus != 200) {
			fmt.Println(AddColor("FAILED", red))
			if respError, flag := respObject["error"].(string); flag != false {
				fmt.Println("Error: ", respError)
			}
			if respMessage, flag := respObject["description"].(string); flag != false {
				fmt.Println("Message: ", respMessage)
			}
		}
	}

	if resp.Status == constants.AcceptedHttpStatusResponse {
		fmt.Println(AddColor("OK", green))
		fmt.Println("Restore has been aborted for the instance name:", color.CyanString(serviceInstanceName))
	}

	if resp.Status == constants.OKHttpStatusResponse {
		fmt.Println("currently no restore in progress for this service instance")
	}

	errors.ErrorIsNil(err)
}
