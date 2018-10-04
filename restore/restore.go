package restore

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/cloudfoundry-incubator/service-fabrik-cli-plugin/constants"
	"github.com/cloudfoundry-incubator/service-fabrik-cli-plugin/errors"
	"github.com/cloudfoundry-incubator/service-fabrik-cli-plugin/guidTranslator"
	"github.com/cloudfoundry-incubator/service-fabrik-cli-plugin/helper"
	"github.com/cloudfoundry/cli/plugin"
	"github.com/fatih/color"
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

	resp, err := client.Do(req)
	errors.ErrorIsNil(err)
	return resp
}

func GetTrimmedGUID(guid string) (string) {
	guid = strings.TrimRight(guid, ",")
	guid = strings.Trim(guid, "\"")
return guid
}

func (c *RestoreCommand) StartRestore(cliConnection plugin.CliConnection, serviceInstanceName string, backupId string, timeStamp string, isGuidOperation bool, instanceGuidOperation bool, instanceNameOperation bool, instanceGuid string, instanceName string, deletedFlag bool) {
	fmt.Println("Starting restore for ", AddColor(serviceInstanceName, cyan), "...")
	var sourceInstanceId string = ""
	if instanceGuidOperation == true {
		var serviceInstanceName string = ""
		serviceInstanceName = guidTranslator.FindInstanceName(cliConnection, instanceGuid, nil)
		fmt.Println("Instance Name = ", serviceInstanceName)
		if serviceInstanceName == "" {
			fmt.Println(AddColor("FAILED", red))
			fmt.Println("Error - No service instance found with service instance guid = ", instanceGuid)
			os.Exit(7)
		} else {
			sourceInstanceId = instanceGuid
		}
	}
	if instanceNameOperation == true {
		guid := guidTranslator.FindInstanceGuid(cliConnection, instanceName, nil, "")
		guid = GetTrimmedGUID(guid)
		sourceInstanceId = guid
	}
	if deletedFlag == true {
		var guidMap map[string]string = guidTranslator.FindDeletedInstanceGuid(cliConnection, instanceName, nil, "")
		if len(guidMap) > 1 {
			fmt.Println(AddColor("FAILED", constants.Red))
			fmt.Println("" + instanceName + " maps to multiple instance GUIDs, please use 'cf instance-events --delete' to list all instance delete events, get required instance guid from the list and then use 'cf list-backup --guid GUID' to fetch backups list.")
			fmt.Println("Enter 'cf backup' to check the list of commands and their usage.")
			os.Exit(1)
		} else {
			for k, _ := range guidMap {
				guid := k
				guid = strings.Trim(guid, ",")
				guid = strings.Trim(guid, "\"")
				sourceInstanceId = guid
			}
		}
	}
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
		var jsonPrep string = ""
		if sourceInstanceId != "" {
			jsonPrep = `{"time_stamp": "` + epochTime + `", "space_guid": "` + userSpaceGuid + `", "source_instance_id": "` + sourceInstanceId + `"}`
		} else {
			jsonPrep = `{"time_stamp": "` + epochTime + `", "space_guid": "` + userSpaceGuid + `"}`
		}
		var jsonStr = []byte(jsonPrep)
		req_body = bytes.NewBuffer(jsonStr)
	}
	var guid string = guidTranslator.FindInstanceGuid(cliConnection, serviceInstanceName, nil, "")
	guid = GetTrimmedGUID(guid)

	var apiEndpoint string = helper.GetApiEndpoint(helper.ReadConfigJsonFile())
	var broker string = GetBrokerName()
	var extUrl string = GetExtUrl()

	apiEndpoint = strings.Replace(apiEndpoint, "api", broker, 1)

	var url string = apiEndpoint + extUrl + "/service_instances/" + guid + "/restore"
	req, err := http.NewRequest("POST", url, req_body)
	var resp *http.Response = GetResponse(client, req)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	if resp.Status != "202 Accepted" {
		fmt.Println(AddColor("FAILED", red))
		var message string = string(body)
		var parts []string = strings.Split(message, ":")
		if len(parts) == 4 {
			fmt.Println("Error - ", parts[3])
		}
	}

	if resp.Status == "202 Accepted" {
		fmt.Println(AddColor("OK", green))
		if isGuidOperation == true {
			fmt.Println("Restore has been initiated for the instance name:", AddColor(serviceInstanceName, cyan), " and from the backup id:", AddColor(backupId, cyan))
		} else {
			fmt.Println("Restore has been initiated for the instance name:", AddColor(serviceInstanceName, cyan), " using time stamp:", AddColor(timeStamp, cyan))
		}
		fmt.Println("Please check the status of restore by entering 'cf service SERVICE_INSTANCE_NAME'")
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
	guid = GetTrimmedGUID(guid)

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
	if (resp.Status != "202 Accepted") && (resp.Status != "200 OK") {
		fmt.Println(AddColor("FAILED", red))
		var message string = string(body)
		var parts []string = strings.Split(message, ":")
		if len(parts) == 4 {
                	fmt.Println("Error - ", parts[3])
                }
	}

	if resp.Status == "202 Accepted" {
		fmt.Println(AddColor("OK", green))
		fmt.Println("Restore has been aborted for the instance name:", color.CyanString(serviceInstanceName))
	}

	if resp.Status == "200 OK" {
		fmt.Println("currently no restore in progress for this service instance")
	}

	errors.ErrorIsNil(err)
}
