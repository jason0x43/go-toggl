/*
Package toggl provides an API for interacting with the Toggl time tracking service.

See https://github.com/toggl/toggl_api_docs for more information on Toggl's REST API.
*/
package toggl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

// Toggl service constants
const (
	TogglAPI       = "https://api.track.toggl.com/api/v9"
	ReportsAPI     = "https://api.track.toggl.com/reports/api/v2"
	DefaultAppName = "go-toggl"
)

type resourceType int

const (
	clients resourceType = iota
	projects
	tags
	timeEntries
)

var resourceTypeMap = map[resourceType]string{
	clients:     "clients",
	projects:    "projects",
	tags:        "tags",
	timeEntries: "time_entries",
}

func (r resourceType) String() string {
	return resourceTypeMap[r]
}

func generateUserResourceURL(resourceType resourceType) string {
	return fmt.Sprintf("/me/%s", resourceType)
}

func generateResourceURL(resourceType resourceType, wid int) string {
	return fmt.Sprintf("/workspaces/%d/"+resourceType.String(), wid)
}

func generateResourceURLWithID(resourceType resourceType, wid int, id int) string {
	return generateResourceURL(resourceType, wid) + fmt.Sprintf("/%d", id)
}

var (
	dlog   = log.New(os.Stderr, "[toggl] ", log.LstdFlags)
	client = &http.Client{}

	// AppName is the application name used when creating timers.
	AppName = DefaultAppName
)

// structures ///////////////////////////

// Session represents an active connection to the Toggl REST API.
type Session struct {
	APIToken string
	username string
	password string
}

// Account represents a user account.
type Account struct {
	APIToken        string      `json:"api_token"`
	Timezone        string      `json:"timezone"`
	ID              int         `json:"id"`
	Workspaces      []Workspace `json:"workspaces"`
	Clients         []Client    `json:"clients"`
	Projects        []Project   `json:"projects"`
	Tasks           []Task      `json:"tasks"`
	Tags            []Tag       `json:"tags"`
	TimeEntries     []TimeEntry `json:"time_entries"`
	BeginningOfWeek int         `json:"beginning_of_week"`
}

// Workspace represents a user workspace.
type Workspace struct {
	ID              int    `json:"id"`
	RoundingMinutes int    `json:"rounding_minutes"`
	Rounding        int    `json:"rounding"`
	Name            string `json:"name"`
	Premium         bool   `json:"premium"`
}

// Client represents a client.
type Client struct {
	Wid      int    `json:"wid"`
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
	Notes    string `json:"notes"`
}

// Project represents a project.
type Project struct {
	Wid             int        `json:"workspace_id"`
	ID              int        `json:"id"`
	Cid             *int       `json:"client_id,omitempty"`
	Name            string     `json:"name"`
	Active          bool       `json:"active"`
	Billable        *bool      `json:"billable,omitempty"`
	ServerDeletedAt *time.Time `json:"server_deleted_at,omitempty"`
}

// IsActive indicates whether a project exists and is active
func (p *Project) IsActive() bool {
	return p.Active && p.ServerDeletedAt == nil
}

// Task represents a task.
type Task struct {
	Wid  int    `json:"wid"`
	Pid  int    `json:"pid"`
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Tag represents a tag.
type Tag struct {
	Wid  int    `json:"workspace_id"`
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// TimeEntry represents a single time entry.
type TimeEntry struct {
	Wid         int        `json:"workspace_id,omitempty"`
	ID          int        `json:"id,omitempty"`
	Pid         *int       `json:"project_id,omitempty"`
	Tid         *int       `json:"task_id,omitempty"`
	Description string     `json:"description,omitempty"`
	Stop        *time.Time `json:"stop,omitempty"`
	Start       *time.Time `json:"start,omitempty"`
	Tags        []string   `json:"tags"`
	Duration    int64      `json:"duration,omitempty"`
	DurOnly     bool       `json:"duronly"`
	Billable    bool       `json:"billable"`
}

type DetailedTimeEntry struct {
	ID              int        `json:"id"`
	Pid             int        `json:"pid"`
	Tid             int        `json:"tid"`
	Uid             int        `json:"uid"`
	User            string     `json:"user,omitempty"`
	Description     string     `json:"description"`
	Project         string     `json:"project"`
	ProjectColor    string     `json:"project_color"`
	ProjectHexColor string     `json:"project_hex_color"`
	Client          string     `json:"client"`
	Start           *time.Time `json:"start"`
	End             *time.Time `json:"end"`
	Updated         *time.Time `json:"updated"`
	Duration        int64      `json:"dur"`
	Billable        bool       `json:"billable"`
	Tags            []string   `json:"tags"`
}

// SummaryReport represents a summary report generated by Toggl's reporting API.
type SummaryReport struct {
	TotalGrand int `json:"total_grand"`
	Data       []struct {
		ID    int `json:"id"`
		Time  int `json:"time"`
		Title struct {
			Project  string `json:"project"`
			Client   string `json:"client"`
			Color    string `json:"color"`
			HexColor string `json:"hex_color"`
		} `json:"title"`
		Items []struct {
			Title map[string]string `json:"title"`
			Time  int               `json:"time"`
		} `json:"items"`
	} `json:"data"`
}

// DetailedReport represents a summary report generated by Toggl's reporting API.
type DetailedReport struct {
	TotalGrand int                 `json:"total_grand"`
	TotalCount int                 `json:"total_count"`
	PerPage    int                 `json:"per_page"`
	Data       []DetailedTimeEntry `json:"data"`
}

// functions ////////////////////////////

// OpenSession opens a session using an existing API token.
func OpenSession(apiToken string) Session {
	return Session{APIToken: apiToken}
}

// NewSession creates a new session by retrieving a user's API token.
func NewSession(username, password string) (session Session, err error) {
	session.username = username
	session.password = password

	data, err := session.get(TogglAPI, "/me", nil)
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
	session.APIToken = account.APIToken

	return session, nil
}

// GetAccount returns a user's account information, including a list of active
// projects and timers.
func (session *Session) GetAccount() (Account, error) {
	params := map[string]string{"with_related_data": "true"}
	data, err := session.get(TogglAPI, "/me", params)
	if err != nil {
		return Account{}, err
	}

	var account Account
	err = decodeAccount(data, &account)
	return account, err
}

// GetSummaryReport retrieves a summary report using Toggle's reporting API.
func (session *Session) GetSummaryReport(
	workspace int,
	since, until string,
) (SummaryReport, error) {
	params := map[string]string{
		"user_agent":   "jc-toggl",
		"grouping":     "projects",
		"since":        since,
		"until":        until,
		"rounding":     "on",
		"workspace_id": fmt.Sprintf("%d", workspace)}
	data, err := session.get(ReportsAPI, "/summary", params)
	if err != nil {
		return SummaryReport{}, err
	}
	dlog.Printf("Got data: %s", data)

	var report SummaryReport
	err = decodeSummaryReport(data, &report)
	return report, err
}

// GetDetailedReport retrieves a detailed report using Toggle's reporting API.
func (session *Session) GetDetailedReport(
	workspace int,
	since, until string,
	page int,
) (DetailedReport, error) {
	params := map[string]string{
		"user_agent":   "jc-toggl",
		"since":        since,
		"until":        until,
		"page":         fmt.Sprintf("%d", page),
		"rounding":     "on",
		"workspace_id": fmt.Sprintf("%d", workspace)}
	data, err := session.get(ReportsAPI, "/details", params)
	if err != nil {
		return DetailedReport{}, err
	}
	dlog.Printf("Got data: %s", data)

	var report DetailedReport
	err = decodeDetailedReport(data, &report)
	return report, err
}

type timeEntryCreate struct {
	Billable    bool       `json:"billable"`
	Description string     `json:"description"`
	Duration    int        `json:"duration"`
	ProjectID   *int       `json:"project_id,omitempty"`
	TaskID      *int       `json:"task_id,omitempty"`
	Start       *time.Time `json:"start,omitempty"`
	Stop        *time.Time `json:"stop,omitempty"`
	Tags        []string   `json:"tags"`
	WorkspaceId int        `json:"workspace_id"`
}

func (t timeEntryCreate) MarshalJSON() ([]byte, error) {
	type Alias timeEntryCreate
	return json.Marshal(&struct {
		Alias
		CreatedWith string `json:"created_with"`
	}{
		Alias:       (Alias)(t),
		CreatedWith: AppName,
	})
}

func (t timeEntryCreate) withMetadataFromTimeEntry(timeEntry TimeEntry) timeEntryCreate {
	t.ProjectID = timeEntry.Pid
	t.TaskID = timeEntry.Tid
	t.Tags = timeEntry.Tags
	t.Billable = timeEntry.Billable

	return t
}

func newStartEntryRequestData(description string, workspaceId int) timeEntryCreate {
	now := time.Now()
	return timeEntryCreate{
		Duration:    -1,
		Description: description,
		Start:       &now,
		WorkspaceId: workspaceId,
	}
}

// startTimeEntry unified way how to start new entries. Eventually it should replace StartTimeEntry and
// StartTimeEntryForProject functions, which are for time-being kept for compatibility.
func (session *Session) startTimeEntry(timeEntry timeEntryCreate) (TimeEntry, error) {
	return handleTimeEntryResponse(
		session.post(TogglAPI, generateResourceURL(timeEntries, timeEntry.WorkspaceId), timeEntry),
	)
}

// StartTimeEntry creates a new time entry.
func (session *Session) StartTimeEntry(description string, wid int) (TimeEntry, error) {
	return session.startTimeEntry(newStartEntryRequestData(description, wid))
}

// StartTimeEntryForProject creates a new time entry for a specific project. Note that the 'billable' option is only
// meaningful for Toggl Pro accounts; it will be ignored for free accounts.
func (session *Session) StartTimeEntryForProject(
	description string,
	wid int,
	projectID int,
	billable *bool,
) (TimeEntry, error) {
	entry := newStartEntryRequestData(description, wid)
	entry.ProjectID = &projectID

	if billable != nil {
		entry.Billable = *billable
	}

	return session.startTimeEntry(entry)
}

// GetCurrentTimeEntry returns the current time entry, that's running
func (session *Session) GetCurrentTimeEntry() (TimeEntry, error) {
	return handleTimeEntryResponse(
		session.get(TogglAPI, generateUserResourceURL(timeEntries)+"/current", nil),
	)
}

// GetTimeEntries returns a list of time entries
func (session *Session) GetTimeEntries(startDate, endDate time.Time) ([]TimeEntry, error) {
	data, err := session.get(
		TogglAPI,
		generateUserResourceURL(timeEntries),
		map[string]string{
			"start_date": startDate.Format(time.RFC3339),
			"end_date":   endDate.Format(time.RFC3339),
		},
	)

	if err != nil {
		return nil, err
	}

	var results []TimeEntry
	err = json.Unmarshal(data, &results)
	if err != nil {
		return nil, err
	}

	return results, nil
}

// UpdateTimeEntry changes information about an existing time entry.
func (session *Session) UpdateTimeEntry(timer TimeEntry) (TimeEntry, error) {
	dlog.Printf("Updating timer %v", timer)
	return handleTimeEntryResponse(
		session.put(TogglAPI, generateResourceURLWithID(timeEntries, timer.Wid, timer.ID), timer),
	)
}

// ContinueTimeEntry continues a time entry, either by creating a new entry
// with the same description or by extending the duration of an existing entry.
// In both cases the new entry will have the same description and project ID as
// the existing one.
func (session *Session) ContinueTimeEntry(timer TimeEntry, duronly bool) (TimeEntry, error) {
	dlog.Printf("Continuing timer %v", timer)
	if duronly &&
		time.Now().Local().Format("2006-01-02") == timer.Start.Local().Format("2006-01-02") {
		// If we're doing a duration-only continuation for a timer today, then basically only unstop the timer
		return session.UnstopTimeEntry(timer)
	} else {
		// If we're not doing a duration-only continuation, or a duration timer
		// wasn't created today, start new time entry with same metadata
		entry := newStartEntryRequestData(timer.Description, timer.Wid)
		entry = entry.withMetadataFromTimeEntry(timer)

		return session.startTimeEntry(entry)
	}
}

// UnstopTimeEntry starts a new entry that is a copy of the given one, including
// the given timer's start time. The given time entry is then deleted.
func (session *Session) UnstopTimeEntry(timer TimeEntry) (newEntry TimeEntry, err error) {
	dlog.Printf("Unstopping timer %v", timer)

	entry := newStartEntryRequestData(timer.Description, timer.Wid)
	entry = entry.withMetadataFromTimeEntry(timer)
	entry.Start = timer.Start

	newEntry, err = session.startTimeEntry(entry)
	if _, err = session.DeleteTimeEntry(timer); err != nil {
		err = fmt.Errorf("old entry not deleted: %v", err)
	}

	return
}

// StopTimeEntry stops a running time entry.
func (session *Session) StopTimeEntry(timer TimeEntry) (TimeEntry, error) {
	dlog.Printf("Stopping timer %v", timer)
	return handleTimeEntryResponse(
		session.patch(
			TogglAPI,
			generateResourceURLWithID(timeEntries, timer.Wid, timer.ID)+"/stop",
		),
	)
}

// AddRemoveTag adds or removes a tag from the time entry corresponding to a
// given ID.
func (session *Session) AddRemoveTag(
	timeEntryId int,
	tag string,
	add bool,
	wid int,
) (TimeEntry, error) {
	dlog.Printf("Adding tag to time entry %v", timeEntryId)

	action := "add"
	if !add {
		action = "remove"
	}

	data := map[string]interface{}{
		"tags":       []string{tag},
		"tag_action": action,
	}

	return handleTimeEntryResponse(
		session.put(TogglAPI, generateResourceURLWithID(timeEntries, wid, timeEntryId), data),
	)
}

// DeleteTimeEntry deletes a time entry.
func (session *Session) DeleteTimeEntry(timer TimeEntry) ([]byte, error) {
	dlog.Printf("Deleting timer %v", timer)
	return session.delete(TogglAPI, generateResourceURLWithID(timeEntries, timer.Wid, timer.ID))
}

// IsRunning returns true if the receiver is currently running.
func (e *TimeEntry) IsRunning() bool {
	return e.Duration < 0
}

// GetProjects allows to query for all projects in a workspace
func (session *Session) GetProjects(wid int) ([]Project, error) {
	dlog.Printf("Getting projects for workspace %d", wid)
	data, err := session.get(TogglAPI, generateResourceURL(projects, wid), nil)
	if err != nil {
		return nil, err
	}

	var projects []Project
	err = json.Unmarshal(data, &projects)
	dlog.Printf("Unmarshaled '%s' into %#v\n", data, projects)
	if err != nil {
		return nil, err
	}

	return projects, nil
}

// GetProject allows to query for all projects in a workspace
func (session *Session) GetProject(id int, wid int) (project Project, err error) {
	dlog.Printf("Getting project with id %d", id)
	data, err := session.get(TogglAPI, generateResourceURLWithID(projects, wid, id), nil)
	if err != nil {
		return project, err
	}

	err = json.Unmarshal(data, &project)
	dlog.Printf("Unmarshaled '%s' into %#v\n", data, project)
	if err != nil {
		return project, err
	}

	return project, nil
}

// CreateProject creates a new project.
func (session *Session) CreateProject(name string, wid int) (project Project, err error) {
	dlog.Printf("Creating project %s", name)
	data := map[string]interface{}{
		"name":   name,
		"wid":    wid,
		"active": true,
	}

	respData, err := session.post(TogglAPI, generateResourceURL(projects, wid), data)
	if err != nil {
		return project, err
	}

	err = json.Unmarshal(respData, &project)
	dlog.Printf("Unmarshaled '%s' into %#v\n", respData, project)
	if err != nil {
		return project, err
	}

	return project, nil
}

// UpdateProject changes information about an existing project.
func (session *Session) UpdateProject(project Project) (Project, error) {
	dlog.Printf("Updating project %v", project)
	respData, err := session.put(
		TogglAPI,
		generateResourceURLWithID(projects, project.Wid, project.ID),
		project,
	)

	if err != nil {
		return Project{}, err
	}

	var entry Project
	err = json.Unmarshal(respData, &entry)
	dlog.Printf("Unmarshaled '%v' into %#v\n", project, entry)
	if err != nil {
		return Project{}, err
	}

	return entry, nil
}

// DeleteProject deletes a project.
func (session *Session) DeleteProject(project Project) ([]byte, error) {
	dlog.Printf("Deleting project %v", project)
	return session.delete(TogglAPI, generateResourceURLWithID(projects, project.Wid, project.ID))
}

// CreateTag creates a new tag.
func (session *Session) CreateTag(name string, wid int) (tag Tag, err error) {
	dlog.Printf("Creating tag %s", name)
	data := map[string]interface{}{
		"name": name,
		"wid":  wid,
	}

	respData, err := session.post(TogglAPI, generateResourceURL(tags, wid), data)
	if err != nil {
		return tag, err
	}

	err = json.Unmarshal(respData, &tag)
	dlog.Printf("Unmarshaled '%s' into %#v\n", respData, tag)
	if err != nil {
		return tag, err
	}

	return tag, nil
}

// UpdateTag changes information about an existing tag.
func (session *Session) UpdateTag(tag Tag) (Tag, error) {
	dlog.Printf("Updating tag %v", tag)
	respData, err := session.put(TogglAPI, generateResourceURLWithID(tags, tag.Wid, tag.ID), tag)

	if err != nil {
		return Tag{}, err
	}

	var entry Tag
	err = json.Unmarshal(respData, &entry)
	dlog.Printf("Unmarshaled '%s' into %#v\n", respData, entry)
	if err != nil {
		return Tag{}, err
	}

	return entry, nil
}

// DeleteTag deletes a tag.
func (session *Session) DeleteTag(tag Tag) ([]byte, error) {
	dlog.Printf("Deleting tag %v", tag)
	return session.delete(TogglAPI, generateResourceURLWithID(tags, tag.Wid, tag.ID))
}

// GetClients returns a list of clients for the current account
func (session *Session) GetClients(wid int) (list []Client, err error) {
	dlog.Println("Retrieving clients")

	data, err := session.get(TogglAPI, generateResourceURL(clients, wid), nil)
	if err != nil {
		return list, err
	}
	err = json.Unmarshal(data, &list)
	return list, err
}

// CreateClient adds a new client
func (session *Session) CreateClient(name string, wid int) (client Client, err error) {
	dlog.Printf("Creating client %s", name)
	data := map[string]interface{}{
		"name": name,
		"wid":  wid,
	}

	respData, err := session.post(TogglAPI, generateResourceURL(clients, wid), data)
	if err != nil {
		return client, err
	}

	err = json.Unmarshal(respData, &client)
	dlog.Printf("Unmarshaled '%s' into %#v\n", respData, client)
	if err != nil {
		return client, err
	}
	return client, nil
}

// Copy returns a copy of a TimeEntry.
func (e *TimeEntry) Copy() TimeEntry {
	newEntry := *e
	newEntry.Tags = make([]string, len(e.Tags))
	copy(newEntry.Tags, e.Tags)
	if e.Start != nil {
		newEntry.Start = &(*e.Start)
	}
	if e.Stop != nil {
		newEntry.Stop = &(*e.Stop)
	}
	return newEntry
}

// StartTime returns the start time of a time entry as a time.Time.
func (e *TimeEntry) StartTime() time.Time {
	if e.Start != nil {
		return *e.Start
	}
	return time.Time{}
}

// StopTime returns the stop time of a time entry as a time.Time.
func (e *TimeEntry) StopTime() time.Time {
	if e.Stop != nil {
		return *e.Stop
	}
	return time.Time{}
}

// HasTag returns true if a time entry contains a given tag.
func (e *TimeEntry) HasTag(tag string) bool {
	return indexOfTag(tag, e.Tags) != -1
}

// AddTag adds a tag to a time entry if the entry doesn't already contain the
// tag.
func (e *TimeEntry) AddTag(tag string) {
	if !e.HasTag(tag) {
		e.Tags = append(e.Tags, tag)
	}
}

// RemoveTag removes a tag from a time entry.
func (e *TimeEntry) RemoveTag(tag string) {
	if i := indexOfTag(tag, e.Tags); i != -1 {
		e.Tags = append(e.Tags[:i], e.Tags[i+1:]...)
	}
}

// SetDuration sets a time entry's duration. The duration should be a value in
// seconds. The stop time will also be updated. Note that the time entry must
// not be running.
func (e *TimeEntry) SetDuration(duration int64) error {
	if e.IsRunning() {
		return fmt.Errorf("TimeEntry must be stopped")
	}

	e.Duration = duration
	newStop := e.Start.Add(time.Duration(duration) * time.Second)
	e.Stop = &newStop

	return nil
}

// SetStartTime sets a time entry's start time. If the time entry is stopped,
// the stop time will also be updated.
func (e *TimeEntry) SetStartTime(start time.Time, updateEnd bool) {
	e.Start = &start

	if !e.IsRunning() {
		if updateEnd {
			newStop := start.Add(time.Duration(e.Duration) * time.Second)
			e.Stop = &newStop
		} else {
			e.Duration = e.Stop.Unix() - e.Start.Unix()
		}
	}
}

// SetStopTime sets a time entry's stop time. The duration will also be
// updated. Note that the time entry must not be running.
func (e *TimeEntry) SetStopTime(stop time.Time) (err error) {
	if e.IsRunning() {
		return fmt.Errorf("TimeEntry must be stopped")
	}

	e.Stop = &stop
	e.Duration = int64(stop.Sub(*e.Start) / time.Second)

	return nil
}

func indexOfTag(tag string, tags []string) int {
	for i, t := range tags {
		if t == tag {
			return i
		}
	}
	return -1
}

// UnmarshalJSON unmarshals a TimeEntry from JSON data, converting timestamp
// fields to Go Time values.
func (e *TimeEntry) UnmarshalJSON(b []byte) error {
	var entry tempTimeEntry
	err := json.Unmarshal(b, &entry)
	if err != nil {
		return err
	}
	te, err := entry.asTimeEntry()
	if err != nil {
		return err
	}
	*e = te
	return nil
}

// support /////////////////////////////////////////////////////////////

func (session *Session) request(method string, requestURL string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, requestURL, body)

	if session.APIToken != "" {
		req.SetBasicAuth(session.APIToken, "api_token")
	} else {
		req.SetBasicAuth(session.username, session.password)
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return content, fmt.Errorf(resp.Status)
	}

	return content, nil
}

func (session *Session) get(
	requestURL string,
	path string,
	params map[string]string,
) ([]byte, error) {
	requestURL += path

	if params != nil {
		data := url.Values{}
		for key, value := range params {
			data.Set(key, value)
		}
		requestURL += "?" + data.Encode()
	}

	dlog.Printf("GETing from URL: %s", requestURL)
	return session.request("GET", requestURL, nil)
}

func (session *Session) post(requestURL string, path string, data interface{}) ([]byte, error) {
	requestURL += path
	var body []byte
	var err error

	if data != nil {
		body, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
	}

	dlog.Printf("POSTing to URL: %s", requestURL)
	dlog.Printf("data: %s", body)
	return session.request("POST", requestURL, bytes.NewBuffer(body))
}

func (session *Session) put(requestURL string, path string, data interface{}) ([]byte, error) {
	requestURL += path
	var body []byte
	var err error

	if data != nil {
		body, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
	}

	dlog.Printf("PUTing to URL %s: %s", requestURL, string(body))
	return session.request("PUT", requestURL, bytes.NewBuffer(body))
}

func (session *Session) patch(requestURL string, path string) ([]byte, error) {
	requestURL += path
	dlog.Printf("PATCHing to URL %s", requestURL)
	return session.request("PATCH", requestURL, nil)
}

func (session *Session) delete(requestURL string, path string) ([]byte, error) {
	requestURL += path
	dlog.Printf("DELETINGing URL: %s", requestURL)
	return session.request("DELETE", requestURL, nil)
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
	dlog.Printf("Decoding %s", data)
	dec := json.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(&report)
	if err != nil {
		return err
	}
	return nil
}

func decodeDetailedReport(data []byte, report *DetailedReport) error {
	dlog.Printf("Decoding %s", data)
	dec := json.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(&report)
	if err != nil {
		return err
	}
	return nil
}

// This is an alias for TimeEntry that is used in tempTimeEntry to prevent the
// unmarshaler from infinitely recursing while unmarshaling.
type embeddedTimeEntry TimeEntry

// tempTimeEntry is an intermediate type used as for decoding TimeEntries.
type tempTimeEntry struct {
	embeddedTimeEntry
	Stop  string `json:"stop"`
	Start string `json:"start"`
}

func (t *tempTimeEntry) asTimeEntry() (entry TimeEntry, err error) {
	entry = TimeEntry(t.embeddedTimeEntry)

	parseTime := func(s string) (t time.Time, err error) {
		t, err = time.Parse("2006-01-02T15:04:05Z", s)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05-07:00", s)
		}
		return
	}

	if t.Start != "" {
		var start time.Time
		start, err = parseTime(t.Start)
		if err != nil {
			return
		}
		entry.Start = &start
	}

	if t.Stop != "" {
		var stop time.Time
		stop, err = parseTime(t.Stop)
		if err != nil {
			return
		}
		entry.Stop = &stop
	}

	return
}

func handleTimeEntryResponse(data []byte, err error) (TimeEntry, error) {
	if err != nil {
		return TimeEntry{}, err
	}

	var entry TimeEntry
	err = json.Unmarshal(data, &entry)
	dlog.Printf("Unmarshaled '%s' into %#v\n", data, entry)
	if err != nil {
		return TimeEntry{}, err
	}

	return entry, nil
}

// DisableLog disables output to stderr
func DisableLog() {
	dlog.SetFlags(0)
	dlog.SetOutput(io.Discard)
}

// EnableLog enables output to stderr
func EnableLog() {
	logFlags := dlog.Flags()
	dlog.SetFlags(logFlags)
	dlog.SetOutput(os.Stderr)
}
