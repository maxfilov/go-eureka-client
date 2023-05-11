package eureka

import (
	"strconv"
)

type RawRequest struct {
	method       string
	relativePath string
	body         []byte
	cancel       <-chan bool
}
type Applications struct {
	VersionsDelta int           `xml:"versions__delta"`
	AppsHashcode  string        `xml:"apps__hashcode"`
	Applications  []Application `xml:"application,omitempty"`
}
type Application struct {
	Name      string         `xml:"name"`
	Instances []InstanceInfo `xml:"instance"`
}
type Instance struct {
	Instance *InstanceInfo `xml:"instance" json:"instance"`
}
type Port struct {
	Port    int  `xml:",chardata" json:"$"`
	Enabled bool `xml:"enabled,attr" json:"@enabled"`
}
type InstanceInfo struct {
	HostName                      string          `xml:"hostName" json:"hostName"`
	HomePageUrl                   string          `xml:"homePageUrl,omitempty" json:"homePageUrl,omitempty"`
	StatusPageUrl                 string          `xml:"statusPageUrl" json:"statusPageUrl"`
	HealthCheckUrl                string          `xml:"healthCheckUrl,omitempty" json:"healthCheckUrl,omitempty"`
	App                           string          `xml:"app" json:"app"`
	IpAddr                        string          `xml:"ipAddr" json:"ipAddr"`
	VipAddress                    string          `xml:"vipAddress" json:"vipAddress"`
	SecureVipAddress              string          `xml:"secureVipAddress,omitempty" json:"secureVipAddress,omitempty"`
	Status                        string          `xml:"status" json:"status"`
	Port                          *Port           `xml:"port,omitempty" json:"port,omitempty"`
	SecurePort                    *Port           `xml:"securePort,omitempty" json:"securePort,omitempty"`
	DataCenterInfo                *DataCenterInfo `xml:"dataCenterInfo" json:"dataCenterInfo"`
	LeaseInfo                     *LeaseInfo      `xml:"leaseInfo,omitempty" json:"leaseInfo,omitempty"`
	Metadata                      *MetaData       `xml:"metadata,omitempty" json:"metadata,omitempty"`
	IsCoordinatingDiscoveryServer bool            `xml:"isCoordinatingDiscoveryServer,omitempty" json:"isCoordinatingDiscoveryServer,omitempty"`
	LastUpdatedTimestamp          int             `xml:"lastUpdatedTimestamp,omitempty" json:"lastUpdatedTimestamp,omitempty"`
	LastDirtyTimestamp            int             `xml:"lastDirtyTimestamp,omitempty" json:"lastDirtyTimestamp,omitempty"`
	ActionType                    string          `xml:"actionType,omitempty" json:"actionType,omitempty"`
	Overriddenstatus              string          `xml:"overriddenstatus,omitempty" json:"overriddenstatus,omitempty"`
	CountryId                     int             `xml:"countryId,omitempty" json:"countryId,omitempty"`
	InstanceID                    string          `xml:"instanceId,omitempty" json:"instanceId,omitempty"`
}
type DataCenterInfo struct {
	Name     string              `xml:"name" json:"name"`
	Class    string              `xml:"class,attr" json:"@class"`
	Metadata *DataCenterMetadata `xml:"metadata,omitempty" json:"metadata,omitempty"`
}

type DataCenterMetadata struct {
	AmiLaunchIndex   string `xml:"ami-launch-index,omitempty" json:"ami-launch-index,omitempty"`
	LocalHostname    string `xml:"local-hostname,omitempty" json:"local-hostname,omitempty"`
	AvailabilityZone string `xml:"availability-zone,omitempty" json:"availability-zone,omitempty"`
	InstanceId       string `xml:"instance-id,omitempty" json:"instance-id,omitempty"`
	PublicIpv4       string `xml:"public-ipv4,omitempty" json:"public-ipv4,omitempty"`
	PublicHostname   string `xml:"public-hostname,omitempty" json:"public-hostname,omitempty"`
	AmiManifestPath  string `xml:"ami-manifest-path,omitempty" json:"ami-manifest-path,omitempty"`
	LocalIpv4        string `xml:"local-ipv4,omitempty" json:"local-ipv4,omitempty"`
	Hostname         string `xml:"hostname,omitempty" json:"hostname,omitempty"`
	AmiId            string `xml:"ami-id,omitempty" json:"ami-id,omitempty"`
	InstanceType     string `xml:"instance-type,omitempty" json:"instance-type,omitempty"`
}

type LeaseInfo struct {
	EvictionDurationInSecs uint `xml:"evictionDurationInSecs,omitempty" json:"evictionDurationInSecs,omitempty"`
	RenewalIntervalInSecs  int  `xml:"renewalIntervalInSecs,omitempty" json:"renewalIntervalInSecs,omitempty"`
	DurationInSecs         int  `xml:"durationInSecs,omitempty" json:"durationInSecs,omitempty"`
	RegistrationTimestamp  int  `xml:"registrationTimestamp,omitempty" json:"registrationTimestamp,omitempty"`
	LastRenewalTimestamp   int  `xml:"lastRenewalTimestamp,omitempty" json:"lastRenewalTimestamp,omitempty"`
	EvictionTimestamp      int  `xml:"evictionTimestamp,omitempty" json:"evictionTimestamp,omitempty"`
	ServiceUpTimestamp     int  `xml:"serviceUpTimestamp,omitempty" json:"serviceUpTimestamp,omitempty"`
}

func NewRawRequest(method, relativePath string, body []byte, cancel <-chan bool) *RawRequest {
	return &RawRequest{
		method:       method,
		relativePath: relativePath,
		body:         body,
		cancel:       cancel,
	}
}

func NewInstanceInfo(hostName, app, ip string, port int, ttl uint, isSsl bool) *InstanceInfo {
	dataCenterInfo := &DataCenterInfo{
		Name:     "MyOwn",
		Class:    "com.netflix.appinfo.InstanceInfo$DefaultDataCenterInfo",
		Metadata: nil,
	}
	leaseInfo := &LeaseInfo{
		EvictionDurationInSecs: ttl,
	}
	instanceInfo := &InstanceInfo{
		HostName:       hostName,
		App:            app,
		IpAddr:         ip,
		Status:         UP,
		DataCenterInfo: dataCenterInfo,
		LeaseInfo:      leaseInfo,
		Metadata:       nil,
	}
	var stringPort string = ""
	if port != 80 && port != 443 {
		stringPort = ":" + strconv.Itoa(port)
	}
	var protocol string = "http"
	if isSsl {
		protocol = "https"
		instanceInfo.SecureVipAddress = protocol + "://" + hostName + stringPort
		instanceInfo.SecurePort = &Port{
			Port:    port,
			Enabled: true,
		}
	} else {
		instanceInfo.VipAddress = protocol + "://" + hostName + stringPort
		instanceInfo.Port = &Port{
			Port:    port,
			Enabled: true,
		}
	}
	instanceInfo.StatusPageUrl = protocol + "://" + hostName + stringPort + "/info"
	return instanceInfo
}
