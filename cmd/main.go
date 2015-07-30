package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"

	awslogtail ".."
)

func main() {
	config := &aws.Config{}

	var (
		follow    = false
		limit     = 100
		startStr  string
		startTime time.Time
		endStr    string
		endTime   time.Time
	)

	flag.StringVar(&config.Region, "region", config.Region, "AWS region name")
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

	if err := awslogtail.Run(config, flag.Args(), follow, limit, startTime, endTime); err != nil {
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
