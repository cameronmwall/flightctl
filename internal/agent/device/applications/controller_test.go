package applications

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/coreos/ignition/v2/config/util"
	"github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/agent/client"
	"github.com/flightctl/flightctl/internal/agent/device/errors"
	"github.com/flightctl/flightctl/pkg/executer"
	"github.com/flightctl/flightctl/pkg/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type testApp struct {
	name  string
	image string
}

func TestParseApps(t *testing.T) {
	require := require.New(t)
	testCases := []struct {
		name         string
		apps         []testApp
		labels       map[string]string
		wantNames    []string
		wantIDPrefix []string
		wantErr      error
	}{
		{
			name: "valid app type",
			apps: []testApp{{name: "app1", image: "quay.io/org/app1:latest"}},
			labels: map[string]string{
				AppTypeLabel: string(AppCompose),
			},
			wantNames:    []string{"app1"},
			wantIDPrefix: []string{"app1-"},
		},
		{
			name: "unsupported app type",
			apps: []testApp{{name: "app1", image: "quay.io/org/app1:latest"}},
			labels: map[string]string{
				AppTypeLabel: "invalid",
			},
			wantErr: errors.ErrParseAppType,
		},
		{
			name:    "missing app type",
			apps:    []testApp{{name: "app1", image: "quay.io/org/app1:latest"}},
			labels:  map[string]string{},
			wantErr: errors.ErrParseAppType,
		},
		{
			name: "missing app name populated by provider image",
			apps: []testApp{{name: "", image: "quay.io/org/app1:latest"}},
			labels: map[string]string{
				AppTypeLabel: string(AppCompose),
			},
			wantNames:    []string{"quay.io/org/app1:latest"},
			wantIDPrefix: []string{"quay_io_org_app1_latest-"},
		},
		{
			name: "no apps",
			apps: []testApp{},
		},
		{
			name: "multiple apps",
			apps: []testApp{
				{name: "app1", image: "quay.io/org/app1:latest"},
				{name: "", image: "quay.io/org/app2:latest"},
				{name: "app2", image: "quay.io/org/app2:latest"},
			},
			labels: map[string]string{
				AppTypeLabel: string(AppCompose),
			},
			wantNames:    []string{"app1", "quay.io/org/app2:latest", "app2"},
			wantIDPrefix: []string{"app1-", "quay_io_org_app2_latest", "app2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			log := log.NewPrefixLogger("test")
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			spec, err := newTestRenderedDeviceSpec(tc.apps)
			require.NoError(err)
			execMock := executer.NewMockExecuter(ctrl)

			imageConfig, err := newImageConfig(tc.labels)
			t.Logf("imageConfig: %s", imageConfig)
			require.NoError(err)
			execMock.EXPECT().ExecuteWithContext(gomock.Any(), "podman", "inspect", gomock.Any()).Return(imageConfig, "", 0).Times(len(tc.apps))

			mockPodman := client.NewPodman(log, execMock, newTestBackoff())
			apps, err := parseApps(ctx, mockPodman, spec)
			if tc.wantErr != nil {
				require.ErrorIs(err, tc.wantErr)
				return
			}
			require.NoError(err)
			require.Equal(len(tc.apps), len(apps.ImageBased()))
			// ensure name is populated
			for i, app := range apps.ImageBased() {
				require.NotEmpty(app.Name())
				if len(tc.wantNames) > 0 {
					require.Equal(tc.wantNames[i], app.Name())
				}
				if len(tc.wantIDPrefix) > 0 {
					require.True(strings.HasPrefix(app.ID(), tc.wantIDPrefix[i]))
				}
			}
		})
	}
}

func newImageConfig(labels map[string]string) (string, error) {
	type inspect struct {
		Config client.ImageConfig `json:"Config"`
	}

	inspectData := []inspect{
		{
			Config: client.ImageConfig{
				Labels: labels,
			},
		},
	}

	imageConfigBytes, err := json.Marshal(inspectData)
	if err != nil {
		return "", err
	}
	return string(imageConfigBytes), nil
}

func newTestRenderedDeviceSpec(appSpecs []testApp) (*v1alpha1.RenderedDeviceSpec, error) {
	var applications []v1alpha1.RenderedApplicationSpec
	for _, spec := range appSpecs {
		app := v1alpha1.RenderedApplicationSpec{
			Name: util.StrToPtr(spec.name),
		}
		provider := v1alpha1.ImageApplicationProvider{
			Image: spec.image,
		}
		if err := app.FromImageApplicationProvider(provider); err != nil {
			return nil, err
		}
		applications = append(applications, app)
	}
	return &v1alpha1.RenderedDeviceSpec{
		Applications: &applications,
	}, nil
}
