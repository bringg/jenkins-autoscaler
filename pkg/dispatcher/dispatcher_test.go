package dispatcher_test

import (
	"context"
	"time"

	mock_dispatcher "github.com/bringg/jenkins-autoscaler/pkg/testing/mocks/dispatcher"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/bringg/jenkins-autoscaler/pkg/dispatcher"
)

var _ = Describe("Dispatcher", func() {
	var disp *dispatcher.Dispatcher
	var ctx context.Context
	var cancelContext context.CancelFunc
	var err error
	var scaler *mock_dispatcher.MockScalerer

	var mockController *gomock.Controller

	BeforeEach(func() {
		ctx, cancelContext = context.WithTimeout(context.Background(), 5*time.Second)

		mockController = gomock.NewController(GinkgoT())

		scaler = mock_dispatcher.NewMockScalerer(mockController)
		cfg := make(configmap.Simple)

		cfg.Set("run_interval", "1s")
		cfg.Set("gc_run_interval", "1s")

		logger, _ := test.NewNullLogger()
		disp, err = dispatcher.New(scaler, cfg, logger, dispatcher.NewMetrics())
		Expect(err).To(Not(HaveOccurred()))
	})

	AfterEach(func() {
		cancelContext()
		mockController.Finish()
	})

	Context("Run", func() {
		It("run dispatcher functionality", func() {
			scaler.EXPECT().Do(gomock.Any()).MinTimes(1)
			scaler.EXPECT().GC(gomock.Any()).MinTimes(1)

			disp.Run(ctx)
		})
	})
})
