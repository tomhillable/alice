package scaler

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"

	"github.com/op/go-logging"

	"errors"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
)

type EC2MetadataAPI interface {
	GetMetadata(string) (string, error)
}

type Scaler struct {
	logger         *logging.Logger
	autoscalingSvc autoscalingiface.AutoScalingAPI
	ec2metadataSvc EC2MetadataAPI
}

type ScalerMetadata struct {
	region       string
	regionWithAZ string
	instanceID   string
}

// NewScaler initialises a new Scaler instance with an AWS session.
func NewScaler(logger *logging.Logger, autoscalingSvc autoscalingiface.AutoScalingAPI, ec2metadataSvc EC2MetadataAPI) (*Scaler, error) {

	return &Scaler{autoscalingSvc: autoscalingSvc, ec2metadataSvc: ec2metadataSvc, logger: logger}, nil
}

func (s *Scaler) Scale(amount int64) error {
	metadata, err := s.metadata()
	if err != nil {
		return err
	}

	var group *autoscaling.Group
	done := false

	params := &autoscaling.DescribeAutoScalingGroupsInput{}
	for !done {
		resp, errD := s.autoscalingSvc.DescribeAutoScalingGroups(params)
		if errD != nil {
			return err
		}

		for _, scaleGroup := range resp.AutoScalingGroups {
			for _, server := range scaleGroup.Instances {
				if *server.InstanceId == metadata.instanceID {
					group = scaleGroup
				}
			}
		}
		if resp.NextToken == nil {
			done = true
		} else {
			params.NextToken = resp.NextToken
		}
	}

	if group == nil {
		return errors.New("scaler: no auto scaling group available")
	}

	// myGroup contains the autoscaling group we live in.
	groupName := *group.AutoScalingGroupName
	currentCapacity := *group.DesiredCapacity
	s.logger.Infof("scaler: current capacity is: %d", currentCapacity)
	newCapacity := currentCapacity + amount
	s.logger.Infof("scaler: new desired capacity will be: %d", newCapacity)

	if newCapacity < *group.MinSize {
		return errors.New("scaler: attempt to scale below minimum capacity denied")
	}

	// This will fail if there's an operation already in place.
	scalingParams := &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(groupName),
		DesiredCapacity:      aws.Int64(newCapacity),
		HonorCooldown:        aws.Bool(true),
	}

	// A successful response is one that doesn't return an error.
	if _, err = s.autoscalingSvc.SetDesiredCapacity(scalingParams); err != nil {
		return err
	}

	s.logger.Infof("scaler: scaling %s to %f slaves", groupName, newCapacity)
	return nil
}

func (s *Scaler) metadata() (*ScalerMetadata, error) {
	instanceID, err := s.ec2metadataSvc.GetMetadata("instance-id")
	if err != nil {
		return nil, err
	}
	regionWithAZ, err := s.ec2metadataSvc.GetMetadata("placement/availability-zone")
	if err != nil {
		return nil, err
	}

	// Strip the AZ from the regionWithAZ to get the region
	region := regionWithAZ[:len(regionWithAZ)-1]

	return &ScalerMetadata{
		instanceID:   instanceID,
		regionWithAZ: regionWithAZ,
		region:       region,
	}, nil
}
