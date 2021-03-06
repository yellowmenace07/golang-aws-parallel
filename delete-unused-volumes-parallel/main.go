package main

import (
	"flag"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"gopkg.in/yaml.v2"
)

//YAMLConfig .. Structure of YAMLConfig type to read yaml data.
type YAMLConfig struct {
	AWSRegion            string `yaml:"aws_region"`
	AWSCredentialFile    string `yaml:"aws_credential_file"`
	AWSCredentialProfile string `yaml:"aws_credential_profile"`
	NoOfExecuter         int    `yaml:"no_of_executer"`
	Duration             int    `yaml:"duration"`
	Dryrun               bool   `yaml:"dryrun"`
	LogLocation          string `yaml:"log_location"`
}

var (
	Log        *log.Logger
	yamlconfig YAMLConfig
)

//WorkerPool .. This function will create worker pool of go routines. It takes data from jobs channel and
//assignes it to a goroutine. Once the task is done by the goroutine, the result is written to results
//channel. Number of goroutine in the worker pool is controlled by the calling function, i.e. main() here.
func WorkerPool(id int, jobs <-chan string, results chan<- string, svc *ec2.EC2, DryRun bool) {
	for j := range jobs {
		Log.Println("Goroutine id", id, "removing snapshot id.", j)
		DeleteUnusedVolumes(j, svc, DryRun)
		results <- j
	}
}

//DeleteUnusedVolumes .. Code to remove ebs volumes
func DeleteUnusedVolumes(volumeID string, svc *ec2.EC2, DryRun bool) {
	params := &ec2.DeleteVolumeInput{
		VolumeId: aws.String(volumeID),
		DryRun:   aws.Bool(DryRun),
	}
	_, err := svc.DeleteVolume(params)
	if err != nil {
		Log.Println(err.Error())
	}

}

func main() {
	var config = flag.String("config", "config.yaml", "Config file path. Please copy the config.yaml to the appropriate path.")
	flag.Parse()

	//Parsing yaml data
	source, FileErr := ioutil.ReadFile(*config)
	if FileErr != nil {
		panic(FileErr)
	}
	FileErr = yaml.Unmarshal(source, &yamlconfig)
	if FileErr != nil {
		panic(FileErr)
	}
	DryRun := yamlconfig.Dryrun
	AWSRegion := yamlconfig.AWSRegion
	AWSCredentialFile := yamlconfig.AWSCredentialFile
	AWSCredentialProfile := yamlconfig.AWSCredentialProfile
	NoOfExecuter := yamlconfig.NoOfExecuter
	Duration := yamlconfig.Duration
	LogLocation := yamlconfig.LogLocation

	// Setting up log path.
	file, FileErr := os.Create(LogLocation)
	if FileErr != nil {
		panic(FileErr)
	}
	Log = log.New(file, "", log.LstdFlags|log.Lshortfile)

	delta := int64(Duration)
	t := time.Now().Unix()

	//Load aws iam credentials
	creds := credentials.NewSharedCredentials(AWSCredentialFile, AWSCredentialProfile)
	_, err := creds.Get()
	if err != nil {
		panic(err)
	}

	// Create an EC2 service object
	svc := ec2.New(session.New(), &aws.Config{
		Region:      aws.String(AWSRegion),
		Credentials: creds,
	})

	resp, err := svc.DescribeVolumes(nil)
	if err != nil {
		panic(err)
	}

	//Creating worker job pool. Number of goroutines to run is configured by -executer command line parameter.
	jobs := make(chan string)
	results := make(chan string)
	for w := 1; w <= NoOfExecuter; w++ {
		go WorkerPool(w, jobs, results, svc, DryRun)
	}

	for _, volume := range resp.Volumes {
		if t-volume.CreateTime.Unix() > delta && *volume.State == "available" {
			Log.Println("Trying to remove volume ", *volume.CreateTime, *volume.VolumeId, *volume.AvailabilityZone, *volume.State)
			go func(volume *ec2.Volume) {
				jobs <- *volume.VolumeId
			}(volume)
			<-results
		}
	}

}
