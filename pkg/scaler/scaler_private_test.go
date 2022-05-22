package scaler

import (
	"github.com/adhocore/gronx"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

var _ = g.DescribeTable("isMinimumNodes", func(numNodes int, cronExpr string, result bool) {
	logger, _ := test.NewNullLogger()
	logger.SetOutput(g.GinkgoWriter)
	logger.SetLevel(logrus.DebugLevel)

	scal := Scaler{
		logger: logger.WithField("test", "private"),
		opt: &Options{
			MinNodesInWorkingHours:      2,
			WorkingHoursCronExpressions: cronExpr,
		},
		schedule: gronx.New(),
	}

	o.Expect(scal.isMinimumNodes(makeFakeNodes(numNodes))).To(o.Equal(result))
},
	g.Entry("is 6 nodes isMinimumNodes in working hours", 6, "* * * * *", true),
	g.Entry("is 2 nodes isMinimumNodes in working hours", 2, "* * * * *", true),
	g.Entry("is 1 nodes isMinimumNodes in working hours", 1, "* * * * *", false),
	g.Entry("is 0 nodes isMinimumNodes in working hours", 0, "* * * * *", false),
	g.Entry("is 1 nodes isMinimumNodes in non working hours", 1, "@yearly", false),
	g.Entry("is 0 nodes isMinimumNodes in non working hours", 0, "@yearly", false),
)
