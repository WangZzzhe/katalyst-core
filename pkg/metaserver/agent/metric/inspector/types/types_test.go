package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseNumastatKey(t *testing.T) {
	t.Parallel()

	testCase := []struct {
		name         string
		key          string
		expectNuma   int
		expectMetric string
		expectErr    bool
	}{
		{
			name:         "numastat_node0_memtotal",
			key:          "numastat_node0_memtotal",
			expectNuma:   0,
			expectMetric: "memtotal",
			expectErr:    false,
		},
		{
			name:         "numastat_node10_memtotal",
			key:          "numastat_node10_memtotal",
			expectNuma:   10,
			expectMetric: "memtotal",
			expectErr:    false,
		},
		{
			name:         "numastat_node10",
			key:          "numastat_node10",
			expectNuma:   0,
			expectMetric: "",
			expectErr:    true,
		},
		{
			name:         "numastat_nodexx_memtotal",
			key:          "numastat_nodexx_memtotal",
			expectNuma:   0,
			expectMetric: "",
			expectErr:    true,
		},
		{
			name:         "numastat_node_memtotal",
			key:          "numastat_node_memtotal",
			expectNuma:   0,
			expectMetric: "",
			expectErr:    true,
		},
	}

	for _, tc := range testCase {
		t.Run(tc.name, func(t *testing.T) {
			numa, metric, err := ParseNumastatKey(tc.key)
			if tc.expectErr {
				assert.NotNil(t, err)
			} else {
				assert.Equal(t, numa, tc.expectNuma)
				assert.Equal(t, metric, tc.expectMetric)
			}
		})
	}
}

func TestParseCorestatKey(t *testing.T) {
	t.Parallel()

	testCase := []struct {
		name         string
		key          string
		expectCpu    int
		expectMetric string
		expectErr    bool
	}{
		{
			name:         "percorecpu_cpu4_usage",
			key:          "percorecpu_cpu4_usage",
			expectCpu:    4,
			expectMetric: "usage",
			expectErr:    false,
		},
		{
			name:         "percorecpu_cpu1_sched_wait",
			key:          "percorecpu_cpu1_sched_wait",
			expectCpu:    1,
			expectMetric: "sched_wait",
			expectErr:    false,
		},
		{
			name:         "percorecpu_cpu1",
			key:          "percorecpu_cpu1",
			expectCpu:    0,
			expectMetric: "",
			expectErr:    true,
		},
	}

	for _, tc := range testCase {
		t.Run(tc.name, func(t *testing.T) {
			numa, metric, err := ParseCorestatKey(tc.key)
			if tc.expectErr {
				assert.NotNil(t, err)
			} else {
				assert.Equal(t, numa, tc.expectCpu)
				assert.Equal(t, metric, tc.expectMetric)
			}
		})
	}
}
