package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
)

func main() {
	var (
		region    = ""
		profile   = ""
		follow    = false
		limit     = 100
		startStr  string
		startTime time.Time
		endStr    string
		endTime   time.Time
		config    aws.Config
	)

	flag.StringVar(&region, "region", region, "AWS region name")
	flag.StringVar(&profile, "profile", profile, "AWS config profile")
	flag.BoolVar(&follow, "f", follow, "output appended data as the logs grow (conflicts with -t and -T)")
	flag.IntVar(&limit, "n", limit, "specify the number of lines to output (conflicts with -T)")
	flag.StringVar(&startStr, "t", startStr, "load messages since YYYY-MM-DDTHH:MM:SS@TZ (conflicts with -f)")
	flag.StringVar(&endStr, "T", endStr, "load messages until YYYY-MM-DDTHH:MM:SS@TZ (conflicts with -n)")
	flag.Parse()

	if startStr != "" {
		startTime = parseTime(startStr)
	}

	if endStr != "" {
		endTime = parseTime(endStr)
	}

	if region != "" {
		config.Region = &region
	}

	if profile != "" {
		config.Credentials = credentials.NewSharedCredentials("", profile)
	}

	sess := session.New(&config)

	if err := Run(sess, flag.Args(), follow, limit, startTime, endTime); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseTime(s string) (t time.Time) {
	var err error

	if t, err = time.Parse("2006-01-02T15:04:05@MST", s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	return
}
