package awslogtail

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/tsavola/pointer"
)

const (
	logEventLimit = 100
	logGroupName  = "/var/log/messages"
	pollInterval  = time.Second * 5
)

func Run(config *aws.Config, filter []string) (err error) {
	logService := cloudwatchlogs.New(config)

	instances, err := ec2.New(config).DescribeInstances(&ec2.DescribeInstancesInput{
		MaxResults: pointer.Int64(1000),
	})
	if err != nil {
		return
	}

	var count int
	initial := make(chan string, 100)
	follow := make(chan string, 100)

	for _, r := range instances.Reservations {
		for _, i := range r.Instances {
			if len(filter) > 0 {
				var name string

				for _, t := range i.Tags {
					if *t.Key == "Name" {
						name = *t.Value
						break
					}
				}

				var match bool

				for _, s := range filter {
					if strings.HasPrefix(name, s) {
						match = true
						break
					}
				}

				if !match {
					continue
				}
			}

			go load(logService, initial, follow, *i.InstanceID, *i.State.Code == 48)
			count++
		}
	}

	if count == 0 {
		err = errors.New("No instances")
		return
	}

	var lines []string

	for line := range initial {
		if line != "" {
			lines = append(lines, line)
		} else {
			count--
			if count == 0 {
				break
			}
		}
	}

	sort.Strings(lines)

	if len(lines) > logEventLimit {
		lines = lines[len(lines)-logEventLimit:]
	}

	for _, line := range lines {
		fmt.Println(line)
	}

	for line := range follow {
		fmt.Println(line)
	}

	return
}

func load(logService *cloudwatchlogs.CloudWatchLogs, initial chan<- string, follow chan<- string, instanceId string, terminated bool) {
	logEvents, err := logService.GetLogEvents(&cloudwatchlogs.GetLogEventsInput{
		EndTime:       pointer.Int64(time.Now().UnixNano() / int64(time.Millisecond)),
		Limit:         pointer.Int64(logEventLimit),
		LogGroupName:  pointer.String(logGroupName),
		LogStreamName: &instanceId,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", instanceId, err)
		initial <- ""
		return
	}

	for _, e := range logEvents.Events {
		if *e.Message != "" {
			initial <- formatMessage(e)
		}
	}

	initial <- ""

	if terminated {
		return
	}

	token := logEvents.NextForwardToken

	for {
		logEvents, err := logService.GetLogEvents(&cloudwatchlogs.GetLogEventsInput{
			LogGroupName:  pointer.String(logGroupName),
			LogStreamName: &instanceId,
			NextToken:     token,
			StartFromHead: pointer.Bool(true),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", instanceId, err)
			continue
		}

		for _, e := range logEvents.Events {
			follow <- formatMessage(e)
		}

		token = logEvents.NextForwardToken

		time.Sleep(pollInterval)
	}
}

func formatMessage(e *cloudwatchlogs.OutputLogEvent) string {
	m := *e.Message

	if len(m) > 16 {
		if _, err := time.Parse("Jan  2 15:04:05 ", m[:16]); err == nil {
			m = m[16:]
		}
	}

	t := time.Unix(0, *e.Timestamp * 1000000)

	return t.Format("2006-01-02 15:04:05 ") + m
}
