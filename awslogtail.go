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

func Run(config *aws.Config, filter []string, doFollow bool, limit int, startTime time.Time) (err error) {
	logService := cloudwatchlogs.New(config)

	instances, err := ec2.New(config).DescribeInstances(&ec2.DescribeInstancesInput{
		MaxResults: pointer.Int64(1000),
	})
	if err != nil {
		return
	}

	var (
		initial = make(chan string, 100)
		follow  chan string
		count   int
	)

	if doFollow {
		follow = make(chan string, 100)
	}

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

			go load(logService, initial, follow, limit, startTime, *i.InstanceID, *i.State.Code == 48)
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

	sort.Stable(byTimestamp(lines))

	if len(lines) > limit {
		if startTime.IsZero() {
			lines = lines[len(lines)-limit:]
		} else {
			lines = lines[:limit]
		}
	}

	for _, line := range lines {
		fmt.Println(line)
	}

	if follow != nil {
		for line := range follow {
			fmt.Println(line)
		}
	}

	return
}

func load(logService *cloudwatchlogs.CloudWatchLogs, initial chan<- string, follow chan<- string, limit int, startTime time.Time, instanceId string, terminated bool) {
	initialParams := &cloudwatchlogs.GetLogEventsInput{
		Limit:         pointer.Int64(int64(limit)),
		LogGroupName:  pointer.String(logGroupName),
		LogStreamName: &instanceId,
	}

	if !startTime.IsZero() {
		initialParams.StartFromHead = pointer.Bool(true)
		initialParams.StartTime = pointer.Int64(startTime.UnixNano() / int64(time.Millisecond))
	} else {
		initialParams.EndTime = pointer.Int64(time.Now().UnixNano() / int64(time.Millisecond))
	}

	logEvents, err := logService.GetLogEvents(initialParams)
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

	if follow == nil || terminated {
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

type byTimestamp []string

func (messages byTimestamp) Len() int {
	return len(messages)
}

func (messages byTimestamp) Swap(i, j int) {
	messages[i], messages[j] = messages[j], messages[i]
}

func (messages byTimestamp) Less(i, j int) bool {
	return messages[i][:19] < messages[j][:19]
}
