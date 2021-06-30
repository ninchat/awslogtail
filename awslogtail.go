package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const (
	logEventLimit = 100
	pollInterval  = time.Second * 5
)

type stream struct {
	name string
	live bool
}

func findEC2Instances(sess *session.Session, filter []string, output chan<- stream) (err error) {
	instances, err := ec2.New(sess).DescribeInstances(&ec2.DescribeInstancesInput{
		MaxResults: aws.Int64(1000),
	})
	if err != nil {
		return
	}

	go func() {
		defer close(output)

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

				output <- stream{
					name: *i.InstanceId,
					live: *i.State.Code != 48,
				}
			}
		}
	}()

	return
}

func findLogStreamsSince(logService *cloudwatchlogs.CloudWatchLogs, logGroup string, startTime, endTime time.Time, output chan<- stream) {
	var (
		startTimestamp = startTime.UnixNano() / 1000000
		endTimestamp   = endTime.UnixNano() / 1000000
	)

	go func() {
		defer close(output)

		var nextToken *string

		for {
			logStreams, err := logService.DescribeLogStreams(&cloudwatchlogs.DescribeLogStreamsInput{
				Descending:   aws.Bool(true),
				LogGroupName: &logGroup,
				NextToken:    nextToken,
				OrderBy:      aws.String("LastEventTime"),
			})
			if err != nil {
				panic(err)
			}

			for _, logStream := range logStreams.LogStreams {
				fmt.Fprintf(os.Stderr, "%s...%s %s\n", time.Unix(0, *logStream.FirstEventTimestamp*1000000), time.Unix(0, *logStream.LastEventTimestamp*1000000), *logStream.LogStreamName)

				if endTimestamp > 0 && *logStream.FirstEventTimestamp > endTimestamp {
					continue
				}

				if *logStream.LastEventTimestamp < startTimestamp {
					return
				}

				output <- stream{
					name: *logStream.LogStreamName,
				}
			}

			nextToken = logStreams.NextToken
		}
	}()

	return
}

func Run(sess *session.Session, logGroup string, filter []string, doFollow bool, limit int, startTime, endTime time.Time) (err error) {
	logService := cloudwatchlogs.New(sess)

	var (
		streams = make(chan stream, 10)
		initial = make(chan string, 100)
		follow  chan string
		count   int
	)

	if doFollow {
		follow = make(chan string, 100)
	}

	if startTime.IsZero() {
		if err = findEC2Instances(sess, filter, streams); err != nil {
			return
		}
	} else {
		findLogStreamsSince(logService, logGroup, startTime, endTime, streams)
	}

	for s := range streams {
		go load(logService, initial, follow, logGroup, limit, startTime, endTime, s.name, !s.live)
		count++
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

	if endTime.IsZero() && len(lines) > limit {
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

func load(logService *cloudwatchlogs.CloudWatchLogs, initial chan<- string, follow chan<- string, logGroup string, limit int, startTime, endTime time.Time, instanceId string, terminated bool) {
	initialParams := &cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  &logGroup,
		LogStreamName: &instanceId,
	}

	if endTime.IsZero() {
		if !startTime.IsZero() {
			initialParams.StartFromHead = aws.Bool(true)
			initialParams.StartTime = aws.Int64(startTime.UnixNano() / int64(time.Millisecond))
		} else {
			initialParams.EndTime = aws.Int64(time.Now().UnixNano() / int64(time.Millisecond))
		}

		initialParams.Limit = aws.Int64(int64(limit))
	} else {
		initialParams.StartFromHead = aws.Bool(true)
		initialParams.StartTime = aws.Int64(startTime.UnixNano() / int64(time.Millisecond))
		initialParams.EndTime = aws.Int64(endTime.UnixNano() / int64(time.Millisecond))
	}

	var token *string

	shouldContinue := func(p *cloudwatchlogs.GetLogEventsOutput, lastPage bool) bool {
		var end bool

		if len(p.Events) == 0 {
			end = true
		}

		for _, e := range p.Events {
			if *e.Message == "" {
				end = true
			} else {
				t, m := formatMessage(e)
				if endTime.IsZero() || t.Before(endTime) {
					initial <- m
				} else {
					end = true
				}
			}
		}

		token = p.NextForwardToken

		if !startTime.IsZero() && !endTime.IsZero() {
			return !end
		} else {
			return false
		}
	}

	err := logService.GetLogEventsPages(initialParams, shouldContinue)
	initial <- ""
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", instanceId, err)
		return
	}

	if follow == nil || terminated {
		return
	}

	for {
		logEvents, err := logService.GetLogEvents(&cloudwatchlogs.GetLogEventsInput{
			LogGroupName:  &logGroup,
			LogStreamName: &instanceId,
			NextToken:     token,
			StartFromHead: aws.Bool(true),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", instanceId, err)
			continue
		}

		for _, e := range logEvents.Events {
			_, m := formatMessage(e)
			follow <- m
		}

		token = logEvents.NextForwardToken

		time.Sleep(pollInterval)
	}
}

func formatMessage(e *cloudwatchlogs.OutputLogEvent) (t time.Time, m string) {
	m = *e.Message

	if len(m) > 16 {
		if _, err := time.Parse("Jan  2 15:04:05 ", m[:16]); err == nil {
			m = m[16:]
		}
	}

	t = time.Unix(0, *e.Timestamp*1000000)
	m = t.Format("2006-01-02 15:04:05 ") + m
	return
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
