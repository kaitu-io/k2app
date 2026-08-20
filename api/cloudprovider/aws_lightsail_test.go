package cloudprovider

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lightsail/types"
	"github.com/stretchr/testify/assert"
)

func TestSumMetricData(t *testing.T) {
	assert.Zero(t, sumMetricData(nil))
	assert.Zero(t, sumMetricData([]types.MetricDatapoint{{Sum: nil}}))
	assert.Equal(t, int64(300), sumMetricData([]types.MetricDatapoint{
		{Sum: aws.Float64(100)},
		{Sum: nil}, // gaps must not crash or contribute
		{Sum: aws.Float64(200)},
	}))
}
