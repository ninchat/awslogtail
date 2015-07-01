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
	)

	flag.StringVar(&config.Region, "region", config.Region, "AWS region name")
	flag.BoolVar(&follow, "f", follow, "output appended data as the logs grow")
	flag.IntVar(&limit, "n", limit, "specify the number of lines to output")
	flag.StringVar(&startStr, "t", startStr, "load a number of messages since YYYY-MM-DDTHH:MM:SS")
	flag.Parse()

	if startStr != "" {
		var err error

		if startTime, err = time.Parse("2006-01-02T15:04:05", startStr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}

	if err := awslogtail.Run(config, flag.Args(), follow, limit, startTime); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
