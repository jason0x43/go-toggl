package toggl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"
)
import (
	"io"
	"io/ioutil"
)

const (
	TogglApi       = "https://toggl.com/api/v8"
	ReportsApi     = "https://toggl.com/reports/api/v2"
	DefaultAppName = "go-toggl"
)

var client = &http.Client{}

//
// Public API
//

// app name used when creating timers
var AppName = DefaultAppName

// structures ///////////////////////////

type Session struct {
	ApiToken string
	username string
	password string
}

type Account struct {
	Data struct {
		ApiToken    string      `json:"api_token"`
		Timezone    string      `json:"timezone"`
		Id          int         `json:"id"`
		Workspaces  []Workspace `json:"workspaces"`
		Projects    []Project   `json:"projects"`
		Tags        []Tag       `json:"tags"`
		TimeEntries []TimeEntry `json:"time_entries"`
	} `json:"data"`
	Since int `json:"since"`
}

type Workspace struct {
	Id              int    `json:"id"`
	RoundingMinutes int    `json:"rounding_minutes"`
	Rounding        int    `json:"rounding"`
	Name            string `json:"name"`
}

type Project struct {
	Wid  int    `json:"wid"`
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type Tag struct {
	Wid  int    `json:"wid"`
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type TimeEntry struct {
	Wid         int        `json:"wid,omitempty"`
	Id          int        `json:"id,omitempty"`
	Pid         int        `json:"pid,omitempty"`
	Description string     `json:"description,omitempty"`
	Stop        *time.Time `json:"stop,omitempty"`
	Start       *time.Time `json:"start,omitempty"`
	Duration    int        `json:"duration,omitempty"`
}

type SummaryReport struct {
	TotalGrand int `json:"total_grand"`
	Data       []struct {
		Id    int `json:"id"`
		Time  int `json:"time"`
		Title struct {
			Project string `json:"project"`
			Client  string `json:"client"`
		} `json:"title"`
		Items []struct {
			Title map[string]string `json:"title"`
			Time  int               `json:"time"`
		} `json:"items"`
	} `json:"data"`
}

// functions ////////////////////////////

func OpenSession(apiToken string) Session {
	return Session{ApiToken: apiToken}
}

func NewSession(username, password string) (session Session, err error) {
	session.username = username
	session.password = password

	data, err := session.get(TogglApi, "/me", nil)
	if err != nil {
		return session, err
	}

	var account Account
	err = decodeAccount(data, &account)
	if err != nil {
		return session, err
	}

	session.username = ""
	session.password = ""
	session.ApiToken = account.Data.ApiToken

	return session, nil
}

func (session *Session) GetAccount() (Account, error) {
	params := map[string]string{"with_related_data": "true"}
	data, err := session.get(TogglApi, "/me", params)
	if err != nil {
		return Account{}, err
	}

	var account Account
	err = decodeAccount(data, &account)
	return account, err
}

func (session *Session) GetSummaryReport(workspace int, since, until string) (SummaryReport, error) {
	params := map[string]string{
		"user_agent":   "jc-toggl",
		"grouping":     "projects",
		"since":        since,
		"until":        until,
		"rounding":     "on",
		"workspace_id": fmt.Sprintf("%d", workspace)}
	data, err := session.get(ReportsApi, "/summary", params)
	if err != nil {
		return SummaryReport{}, err
	}
	log.Printf("Got data: %s", data)

	var report SummaryReport
	err = decodeSummaryReport(data, &report)
	return report, err
}

func timeEntryRequest(data []byte, err error) (TimeEntry, error) {
	if err != nil {
		return TimeEntry{}, err
	}

	var entry struct {
		Data TimeEntry `json:"data"`
	}
	err = json.Unmarshal(data, &entry)
	log.Printf("Unmarshaled '%s' into %#v\n", data, entry)
	if err != nil {
		return TimeEntry{}, err
	}

	return entry.Data, nil
}

func (session *Session) StartTimer(description string) (TimeEntry, error) {
	data := map[string]interface{}{
		"time_entry": map[string]string{
			"description":  description,
			"created_with": AppName,
		},
	}
	respData, err := session.post(TogglApi, "/time_entries/start", data)
	return timeEntryRequest(respData, err)
}

func (session *Session) StartTimerForProject(description string, projectId int) (TimeEntry, error) {
	data := map[string]interface{}{
		"time_entry": map[string]interface{}{
			"description":  description,
			"pid":          projectId,
			"created_with": AppName,
		},
	}
	respData, err := session.post(TogglApi, "/time_entries/start", data)
	return timeEntryRequest(respData, err)
}

func (session *Session) UpdateTimer(timer TimeEntry) (TimeEntry, error) {
	log.Printf("Updating timer %v", timer)
	data := map[string]interface{}{
		"time_entry": timer,
	}
	path := fmt.Sprintf("/time_entries/%v", timer.Id)
	respData, err := session.post(TogglApi, path, data)
	return timeEntryRequest(respData, err)
}

func (session *Session) ContinueTimer(timer TimeEntry) (TimeEntry, error) {
	log.Printf("Continuing timer %v", timer)
	data := map[string]interface{}{
		"time_entry": map[string]interface{}{
			"description":  timer.Description,
			"pid":          timer.Pid,
			"created_with": AppName,
		},
	}
	respData, err := session.post(TogglApi, "/time_entries/start", data)
	return timeEntryRequest(respData, err)
}

func (session *Session) StopTimer(timer TimeEntry) (TimeEntry, error) {
	log.Printf("Stopping timer %v", timer)
	path := fmt.Sprintf("/time_entries/%v/stop", timer.Id)
	respData, err := session.put(TogglApi, path, nil)
	return timeEntryRequest(respData, err)
}

func (session *Session) DeleteTimer(timer TimeEntry) ([]byte, error) {
	log.Printf("Deleting timer %v", timer)
	path := fmt.Sprintf("/time_entries/%v", timer.Id)
	return session.delete(TogglApi, path)
}

func (timer *TimeEntry) IsRunning() bool {
	return timer.Duration < 0
}

func (session *Session) CreateProject(name string, wid int) (proj Project, err error) {
	log.Printf("Creating project %s", name)
	data := map[string]interface{}{
		"project": map[string]interface{}{
			"name": name,
			"wid":  wid,
		},
	}

	respData, err := session.post(TogglApi, "/projects", data)
	if err != nil {
		return proj, err
	}

	var entry struct {
		Data Project `json:"data"`
	}
	err = json.Unmarshal(respData, &entry)
	log.Printf("Unmarshaled '%s' into %#v\n", respData, entry)
	if err != nil {
		return proj, err
	}

	return entry.Data, nil
}

func (session *Session) request(method string, requestUrl string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, requestUrl, body)

	if session.ApiToken != "" {
		req.SetBasicAuth(session.ApiToken, "api_token")
	} else {
		req.SetBasicAuth(session.username, session.password)
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return content, fmt.Errorf(resp.Status)
	}

	return content, nil
}

func (session *Session) get(requestUrl string, path string, params map[string]string) ([]byte, error) {
	requestUrl += path

	if params != nil {
		data := url.Values{}
		for key, value := range params {
			data.Set(key, value)
		}
		requestUrl += "?" + data.Encode()
	}

	log.Printf("GETing from URL: %s", requestUrl)
	return session.request("GET", requestUrl, nil)
}

func (session *Session) post(requestUrl string, path string, data interface{}) ([]byte, error) {
	requestUrl += path
	var body []byte
	var err error

	if data != nil {
		body, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
	}

	log.Printf("POSTing to URL: %s", requestUrl)
	log.Printf("data: %s", body)
	return session.request("POST", requestUrl, bytes.NewBuffer(body))
}

func (session *Session) put(requestUrl string, path string, data interface{}) ([]byte, error) {
	requestUrl += path
	var body []byte
	var err error

	if data != nil {
		body, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
	}

	log.Printf("PUTing to URL: %s", requestUrl)
	return session.request("PUT", requestUrl, bytes.NewBuffer(body))
}

func (session *Session) delete(requestUrl string, path string) ([]byte, error) {
	requestUrl += path
	log.Printf("DELETINGing URL: %s", requestUrl)
	return session.request("DELETE", requestUrl, nil)
}

func decodeSession(data []byte, session *Session) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(session)
	if err != nil {
		return err
	}
	return nil
}

func decodeAccount(data []byte, account *Account) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(account)
	if err != nil {
		return err
	}
	return nil
}

func decodeSummaryReport(data []byte, report *SummaryReport) error {
	log.Printf("Decoding %s", data)
	dec := json.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(&report)
	if err != nil {
		return err
	}
	return nil
}

type tempTimeEntry struct {
	Wid         int    `json:"wid"`
	Id          int    `json:"id"`
	Pid         int    `json:"pid"`
	Description string `json:"description"`
	Duration    int    `json:"duration"`
	Stop        string `json:"stop"`
	Start       string `json:"start"`
}

func (t *tempTimeEntry) asTimeEntry() (TimeEntry, error) {
	entry := TimeEntry{
		Wid:         t.Wid,
		Id:          t.Id,
		Pid:         t.Pid,
		Description: t.Description,
		Duration:    t.Duration,
	}

	parseTime := func(s string) (time.Time, error) {
		t, err := time.Parse("2006-01-02T15:04:05Z", s)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05-07:00", s)
		}
		return t, err
	}

	if t.Start != "" {
		start, err := parseTime(t.Start)
		if err != nil {
			return TimeEntry{}, err
		}
		entry.Start = &start
	}

	if t.Stop != "" {
		stop, err := parseTime(t.Stop)
		if err != nil {
			return TimeEntry{}, err
		}
		entry.Stop = &stop
	}

	return entry, nil
}

func (t *TimeEntry) UnmarshalJSON(b []byte) error {
	var entry tempTimeEntry
	err := json.Unmarshal(b, &entry)
	if err != nil {
		return err
	}
	te, err := entry.asTimeEntry()
	if err != nil {
		return err
	}
	*t = te
	return nil
}
