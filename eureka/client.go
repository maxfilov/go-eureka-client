package eureka

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	UP = "UP"
)

type Config struct {
	CertFile    string        `json:"certFile"`
	KeyFile     string        `json:"keyFile"`
	CaCertFile  []string      `json:"caCertFiles"`
	DialTimeout time.Duration `json:"timeout"`
	Consistency string        `json:"consistency"`
}

type Client struct {
	Config      Config   `json:"config"`
	Cluster     *Cluster `json:"cluster"`
	httpClient  *http.Client
	persistence io.Writer
	cURLch      chan string
	// CheckRetry can be used to control the policy for failed requests
	// and modify the cluster if needed.
	// The client calls it before sending requests again, and
	// stops retrying if CheckRetry returns some error. The cases that
	// this function needs to handle include no response and unexpected
	// http status code of response.
	// If CheckRetry is nil, client will call the default one
	// `DefaultCheckRetry`.
	// Argument cluster is the eureka.Cluster object that these requests have been made on.
	// Argument numReqs is the number of http.Requests that have been made so far.
	// Argument lastResp is the http.Responses from the last request.
	// Argument err is the reason of the failure.
	CheckRetry func(
		cluster *Cluster, numReqs int,
		lastResp http.Response, err error) error
}

// NewClient create a basic client that is configured to be used
// with the given machine list.
func NewClient(machines []string) *Client {
	config := Config{
		// default timeout is one second
		DialTimeout: time.Second,
	}

	client := &Client{
		Cluster: NewCluster(machines),
		Config:  config,
	}

	client.initHTTPClient()
	return client
}

// NewTLSClient create a basic client with TLS configuration
func NewTLSClient(machines []string, cert string, key string, caCerts []string) (*Client, error) {
	// overwrite the default machine to use https
	if len(machines) == 0 {
		machines = []string{"https://127.0.0.1:4001"}
	}

	config := Config{
		// default timeout is one second
		DialTimeout: time.Second,
		CertFile:    cert,
		KeyFile:     key,
		CaCertFile:  make([]string, 0),
	}

	client := &Client{
		Cluster: NewCluster(machines),
		Config:  config,
	}

	err := client.initHTTPSClient(cert, key)
	if err != nil {
		return nil, err
	}

	for _, caCert := range caCerts {
		if err := client.AddRootCA(caCert); err != nil {
			return nil, err
		}
	}
	return client, nil
}

// NewClientFromFile creates a client from a given file path.
// The given file is expected to use the JSON format.
func NewClientFromFile(fpath string) (*Client, error) {
	fi, err := os.Open(fpath)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := fi.Close(); err != nil {
			panic(err)
		}
	}()

	return NewClientFromReader(fi)
}

// NewClientFromReader creates a Client configured from a given reader.
// The configuration is expected to use the JSON format.
func NewClientFromReader(reader io.Reader) (*Client, error) {
	c := new(Client)

	b, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(b, c)
	if err != nil {
		return nil, err
	}
	if c.Config.CertFile == "" {
		c.initHTTPClient()
	} else {
		err = c.initHTTPSClient(c.Config.CertFile, c.Config.KeyFile)
	}

	if err != nil {
		return nil, err
	}

	for _, caCert := range c.Config.CaCertFile {
		if err := c.AddRootCA(caCert); err != nil {
			return nil, err
		}
	}

	return c, nil
}

// Override the Client's HTTP Transport object
func (c *Client) SetTransport(tr *http.Transport) {
	c.httpClient.Transport = tr
}

// initHTTPClient initializes a HTTP client for eureka client
func (c *Client) initHTTPClient() {
	tr := &http.Transport{
		Dial: c.dial,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	c.httpClient = &http.Client{Transport: tr}
}

// initHTTPClient initializes a HTTPS client for eureka client
func (c *Client) initHTTPSClient(cert, key string) error {
	if cert == "" || key == "" {
		return errors.New("Require both cert and key path")
	}

	tlsCert, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return err
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{tlsCert},
		InsecureSkipVerify: true,
	}

	tr := &http.Transport{
		TLSClientConfig: tlsConfig,
		Dial:            c.dial,
	}

	c.httpClient = &http.Client{Transport: tr}
	return nil
}

// Sets the DialTimeout value
func (c *Client) SetDialTimeout(d time.Duration) {
	c.Config.DialTimeout = d
}

// AddRootCA adds a root CA cert for the eureka client
func (c *Client) AddRootCA(caCert string) error {
	if c.httpClient == nil {
		return errors.New("Client has not been initialized yet!")
	}

	certBytes, err := os.ReadFile(caCert)
	if err != nil {
		return err
	}

	tr, ok := c.httpClient.Transport.(*http.Transport)

	if !ok {
		panic("AddRootCA(): Transport type assert should not fail")
	}

	if tr.TLSClientConfig.RootCAs == nil {
		caCertPool := x509.NewCertPool()
		ok = caCertPool.AppendCertsFromPEM(certBytes)
		if ok {
			tr.TLSClientConfig.RootCAs = caCertPool
		}
		tr.TLSClientConfig.InsecureSkipVerify = false
	} else {
		ok = tr.TLSClientConfig.RootCAs.AppendCertsFromPEM(certBytes)
	}

	if !ok {
		err = errors.New("Unable to load caCert")
	}

	c.Config.CaCertFile = append(c.Config.CaCertFile, caCert)
	return err
}

// SetCluster updates cluster information using the given machine list.
func (c *Client) SetCluster(machines []string) bool {
	success := c.internalSyncCluster(machines)
	return success
}

func (c *Client) GetCluster() []string {
	return c.Cluster.Machines
}

// SyncCluster updates the cluster information using the internal machine list.
func (c *Client) SyncCluster() bool {
	return c.internalSyncCluster(c.Cluster.Machines)
}

// internalSyncCluster syncs cluster information using the given machine list.
func (c *Client) internalSyncCluster(machines []string) bool {
	for _, machine := range machines {
		httpPath := c.createHttpPath(machine, "machines")
		resp, err := c.httpClient.Get(httpPath)
		if err != nil {
			// try another machine in the cluster
			continue
		} else {
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				// try another machine in the cluster
				continue
			}
			resp.Body.Close()

			// update Machines List
			c.Cluster.updateFromStr(string(b))

			// update leader
			// the first one in the machine list is the leader
			c.Cluster.switchLeader(0)

			_debugf("sync.machines %s", strings.Join(c.Cluster.Machines, ", "))
			return true
		}
	}
	return false
}

// createHttpPath creates a complete HTTP URL.
// serverName should contain both the host name and a port number, if any.
func (c *Client) createHttpPath(serverName string, _path string) string {
	u, err := url.Parse(serverName)
	if err != nil {
		panic(err)
	}

	u.Path = path.Join(u.Path, _path)

	if u.Scheme == "" {
		u.Scheme = "http"
	}
	return u.String()
}

// dial attempts to open a TCP connection to the provided address, explicitly
// enabling keep-alives with a one-second interval.
func (c *Client) dial(network, addr string) (net.Conn, error) {
	conn, err := net.DialTimeout(network, addr, c.Config.DialTimeout)
	if err != nil {
		return nil, err
	}

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil, errors.New("Failed type-assertion of net.Conn as *net.TCPConn")
	}

	// Keep TCP alive to check whether or not the remote machine is down
	if err = tcpConn.SetKeepAlive(true); err != nil {
		return nil, err
	}

	if err = tcpConn.SetKeepAlivePeriod(time.Second); err != nil {
		return nil, err
	}

	return tcpConn, nil
}

// MarshalJSON implements the Marshaller interface
// as defined by the standard JSON package.
func (c *Client) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(
		struct {
			Config  Config   `json:"config"`
			Cluster *Cluster `json:"cluster"`
		}{
			Config:  c.Config,
			Cluster: c.Cluster,
		})

	if err != nil {
		return nil, err
	}

	return b, nil
}

// UnmarshalJSON implements the Unmarshaller interface
// as defined by the standard JSON package.
func (c *Client) UnmarshalJSON(b []byte) error {
	temp := struct {
		Config  Config   `json:"config"`
		Cluster *Cluster `json:"cluster"`
	}{}
	err := json.Unmarshal(b, &temp)
	if err != nil {
		return err
	}

	c.Cluster = temp.Cluster
	c.Config = temp.Config
	return nil
}

// RegisterInstance
// Register new application instance
func (c *Client) RegisterInstance(appId string, instanceInfo *InstanceInfo) error {
	instance := &Instance{
		Instance: instanceInfo,
	}

	body, err := json.Marshal(instance)
	if err != nil {
		return fmt.Errorf("cannot serialize request body: %w", err)
	}

	resp, err := c.Post("apps/"+appId, body)
	if err != nil {
		return fmt.Errorf("cannot register application instance: %w", err)
	}

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("cannot register application instance: unexpected code: %d", resp.StatusCode)
	}
	return nil
}

// UnregisterInstance De-register application instance
func (c *Client) UnregisterInstance(appId, instanceId string) error {
	resp, err := c.Delete("apps/" + appId + "/" + instanceId)
	if err != nil {
		return fmt.Errorf("cannot de-register application instance: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return ErrInstanceNotFound
	default:
		return fmt.Errorf(
			"cannot de-register application instance: unexpected code: %d",
			resp.StatusCode,
		)
	}
}

// SendHeartbeat Send application instance heartbeat
func (c *Client) SendHeartbeat(appId, instanceId string) error {
	resp, err := c.Put("apps/"+appId+"/"+instanceId, nil)
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return ErrInstanceNotFound
	default:
		return fmt.Errorf("cannot send heartbeat: unexpected code: %d", resp.StatusCode)
	}
}

// GetApplications Query for all instances
func (c *Client) GetApplications() (*Applications, error) {
	resp, err := c.Get("apps")
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve applications: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var applications Applications
		err = xml.Unmarshal(resp.Body, &applications)
		return &applications, err
	default:
		return nil, fmt.Errorf("cannot retrieve applications: unexpected code: %d", resp.StatusCode)
	}
}

// GetApplication Query for all appID instances
func (c *Client) GetApplication(appId string) (*Application, error) {
	resp, err := c.Get("apps/" + appId)
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve application: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var application *Application = new(Application)
		err = xml.Unmarshal(resp.Body, application)
		return application, err
	case http.StatusNotFound:
		return nil, ErrApplicationNotFound
	default:
		return nil, fmt.Errorf("cannot retrieve application: unexpected code: %d", resp.StatusCode)
	}
}

// GetInstance Query for a specific appID/instanceID
func (c *Client) GetInstance(appId, instanceId string) (*InstanceInfo, error) {
	resp, err := c.Get("apps/" + appId + "/" + instanceId)
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve application instance: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var instance InstanceInfo
		err = xml.Unmarshal(resp.Body, &instance)
		return &instance, err
	case http.StatusNotFound:
		return nil, ErrInstanceNotFound
	default:
		return nil, fmt.Errorf("cannot retrieve application instance: unexpected code: %d", resp.StatusCode)
	}
}

// GetVIP Query for all instances under a particular vip address
func (c *Client) GetVIP(vipAddress string) (*Applications, error) {
	resp, err := c.Get("vips/" + vipAddress)
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve instances: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		var applications Applications
		err = xml.Unmarshal(resp.Body, &applications)
		return &applications, err
	case http.StatusNotFound:
		return nil, ErrApplicationNotFound
	default:
		return nil, fmt.Errorf("cannot retrieve instances: unexpected code: %d", resp.StatusCode)
	}
}

// GetSVIP Query for all instances under a particular secure vip address
func (c *Client) GetSVIP(svipAddress string) (*Applications, error) {
	resp, err := c.Get("svips/" + svipAddress)
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve instances: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cannot retrieve instances: unexpected code: %d", resp.StatusCode)
	}
	var applications *Applications = new(Applications)
	err = xml.Unmarshal(resp.Body, applications)
	return applications, err
}

// getCancelable issues a cancelable GET request
func (c *Client) getCancelable(
	endpoint string,
	cancel <-chan bool) (*RawResponse, error) {
	_debugf("get %s [%s]", endpoint, c.Cluster.Leader)
	p := endpoint

	req := NewRawRequest("GET", p, nil, cancel)
	resp, err := c.SendRequest(req)

	if err != nil {
		return nil, err
	}

	return resp, nil
}

// get issues a GET request
func (c *Client) Get(endpoint string) (*RawResponse, error) {
	return c.getCancelable(endpoint, nil)
}

// put issues a PUT request
func (c *Client) Put(endpoint string, body []byte) (*RawResponse, error) {

	_debugf("put %s, %s, [%s]", endpoint, body, c.Cluster.Leader)
	p := endpoint

	req := NewRawRequest("PUT", p, body, nil)
	resp, err := c.SendRequest(req)

	if err != nil {
		return nil, err
	}

	return resp, nil
}

// post issues a POST request
func (c *Client) Post(endpoint string, body []byte) (*RawResponse, error) {
	_debugf("post %s, %s, [%s]", endpoint, body, c.Cluster.Leader)
	p := endpoint

	req := NewRawRequest("POST", p, body, nil)
	resp, err := c.SendRequest(req)

	if err != nil {
		return nil, err
	}

	return resp, nil
}

// delete issues a DELETE request
func (c *Client) Delete(endpoint string) (*RawResponse, error) {
	_debugf("delete %s [%s]", endpoint, c.Cluster.Leader)
	p := endpoint

	req := NewRawRequest("DELETE", p, nil, nil)
	resp, err := c.SendRequest(req)

	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) SendRequest(rr *RawRequest) (*RawResponse, error) {

	var req *http.Request
	var resp *http.Response
	var httpPath string
	var err error
	var respBody []byte

	var numReqs = 1

	checkRetry := c.CheckRetry
	if checkRetry == nil {
		checkRetry = DefaultCheckRetry
	}

	cancelled := make(chan bool, 1)
	reqLock := new(sync.Mutex)

	if rr.cancel != nil {
		cancelRoutine := make(chan bool)
		defer close(cancelRoutine)

		go func() {
			select {
			case <-rr.cancel:
				cancelled <- true
				_debugf("send.request is cancelled")
			case <-cancelRoutine:
				return
			}

			// Repeat canceling request until this thread is stopped
			// because we have no idea about whether it succeeds.
			for {
				reqLock.Lock()
				c.httpClient.Transport.(*http.Transport).CancelRequest(req)
				reqLock.Unlock()

				select {
				case <-time.After(100 * time.Millisecond):
				case <-cancelRoutine:
					return
				}
			}
		}()
	}

	// If we connect to a follower and consistency is required, retry until
	// we connect to a leader
	sleep := 25 * time.Millisecond
	maxSleep := time.Second

	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			select {
			case <-cancelled:
				return nil, ErrRequestCancelled
			case <-time.After(sleep):
				sleep = sleep * 2
				if sleep > maxSleep {
					sleep = maxSleep
				}
			}
		}

		_debugf("Connecting to eureka: attempt %d for %s", attempt+1, rr.relativePath)

		httpPath = c.getHttpPath(false, rr.relativePath)

		_debugf("send.request.to %s | method %s", httpPath, rr.method)

		req, err := func() (*http.Request, error) {
			reqLock.Lock()
			defer reqLock.Unlock()

			if req, err = http.NewRequest(rr.method, httpPath, bytes.NewReader(rr.body)); err != nil {
				return nil, err
			}

			req.Header.Set(
				"Content-Type",
				"application/json")
			return req, nil
		}()

		if err != nil {
			return nil, err
		}

		resp, err = c.httpClient.Do(req)
		defer func() {
			if resp != nil {
				resp.Body.Close()
			}
		}()

		// If the request was cancelled, return ErrRequestCancelled directly
		select {
		case <-cancelled:
			return nil, ErrRequestCancelled
		default:
		}

		numReqs++

		// network error, change a machine!
		if err != nil {
			_errorf("network error: %v", err.Error())
			lastResp := http.Response{}
			if checkErr := checkRetry(c.Cluster, numReqs, lastResp, err); checkErr != nil {
				return nil, checkErr
			}

			c.Cluster.switchLeader(attempt % len(c.Cluster.Machines))
			continue
		}

		// if there is no error, it should receive response
		_debugf("recv.response.from %s", httpPath)

		if validHttpStatusCode[resp.StatusCode] {
			// try to read byte code and break the loop
			respBody, err = io.ReadAll(resp.Body)
			if err == nil {
				_debugf("recv.success %s", httpPath)
				break
			}
			// ReadAll error may be caused due to cancel request
			select {
			case <-cancelled:
				return nil, ErrRequestCancelled
			default:
			}

			if err == io.ErrUnexpectedEOF {
				// underlying connection was closed prematurely, probably by timeout
				// TODO: empty body or unexpectedEOF can cause http.Transport to get hosed;
				// this allows the client to detect that and take evasive action. Need
				// to revisit once code.google.com/p/go/issues/detail?id=8648 gets fixed.
				respBody = []byte{}
				break
			}
		}

		// if resp is TemporaryRedirect, set the new leader and retry
		if resp.StatusCode == http.StatusTemporaryRedirect {
			u, err := resp.Location()

			if err != nil {
				_warningf("%v", err)
			} else {
				// Update cluster leader based on redirect location
				// because it should point to the leader address
				c.Cluster.updateLeaderFromURL(u)
				_debugf("recv.response.relocate %s", u.String())
			}
			resp.Body.Close()
			continue
		}

		if checkErr := checkRetry(
			c.Cluster, numReqs, *resp,
			errors.New("Unexpected HTTP status code")); checkErr != nil {
			return nil, checkErr
		}
		resp.Body.Close()
	}

	r := &RawResponse{
		StatusCode: resp.StatusCode,
		Body:       respBody,
		Header:     resp.Header,
	}

	return r, nil
}

// DefaultCheckRetry defines the retrying behaviour for bad HTTP requests
// If we have retried 2 * machine number, stop retrying.
// If status code is InternalServerError, sleep for 200ms.
func DefaultCheckRetry(
	cluster *Cluster, numReqs int, lastResp http.Response,
	_ error) error {

	if numReqs >= 2*len(cluster.Machines) {
		return ErrEurekaNotReachable
	}

	code := lastResp.StatusCode
	if code == http.StatusInternalServerError {
		time.Sleep(time.Millisecond * 200)

	}

	_warningf("bad response status code %d", code)
	return nil
}

func (c *Client) getHttpPath(random bool, s ...string) string {
	var machine string
	if random {
		machine = c.Cluster.Machines[rand.Intn(len(c.Cluster.Machines))]
	} else {
		machine = c.Cluster.Leader
	}

	fullPath := machine
	for _, seg := range s {
		fullPath += "/" + seg
	}

	return fullPath
}
