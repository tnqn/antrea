package suricata

import (
	"antrea.io/antrea/pkg/agent/controller/l7networkpolicy/types"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/sets"
	"testing"
)

func TestEnforcerAddRule(t *testing.T) {
	type fields struct {
		rules map[string]string
	}
	type args struct {
		name string
		rule *types.Rule
	}
	tests := []struct {
		name              string
		rule              *types.Rule
		expectedSignature string
	}{
		{
			name: "ipblock get any /admin",
			rule: &types.Rule{
				From:   sets.NewString("1.1.1.0/24"),
				To:     nil,
				Action: "drop",
			},
			expectedSignature: "drop http [1.1.1.0/24] any -> any any (sid:1; rev:1;)",
		},
		{
			name: "ips get ips /index",
			rule: &types.Rule{
				From:   sets.NewString("1.1.1.1", "1.1.1.2"),
				To:     sets.NewString("2.2.2.1", "2.2.2.2"),
				Action: "drop",
			},
			expectedSignature: "drop http [1.1.1.1,1.1.1.2] any -> [2.2.2.1,2.2.2.2] any (sid:1; rev:1;)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEnforcer()
			assert.NoError(t, e.AddRule(tt.name, tt.rule))
			assert.Equal(t, tt.expectedSignature, e.rules[tt.name])
		})
	}
}
