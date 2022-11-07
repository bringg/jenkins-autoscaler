# Jenkins AutoScaler

[![build](https://github.com/bringg/jenkins-autoscaler/actions/workflows/test.yml/badge.svg)](https://github.com/bringg/jenkins-autoscaler/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/bringg/jenkins-autoscaler/branch/master/graph/badge.svg?token=eyQ42UjW9z)](https://codecov.io/gh/bringg/jenkins-autoscaler)
[![Go Report Card](https://goreportcard.com/badge/github.com/bringg/jenkins-autoscaler)](https://goreportcard.com/report/github.com/bringg/jenkins-autoscaler)
[![GoDoc](https://godoc.org/github.com/bringg/jenkins-autoscaler?status.svg)](https://godoc.org/github.com/bringg/jenkins-autoscaler)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
![Downloads](https://img.shields.io/github/downloads/bringg/jenkins-autoscaler/total)

Jenkins Autoscaler is a tool that automatically adjusts the size of the Jenkins worker nodes in AWS or GCP when one of the following conditions is true:

  1. If the usage of Jenkins nodes is smaller then the specified threshold, it will scale down or if the usage is larger then a specified threshold, will scale up to the maximum of specified size limit.
  2. Running garbage collector to remove zombie nodes.
  3. When the queue is empty, it checks whether any node can be terminated by establishing whether the node is in idle state.
  4. One node is kept running all the time, apart during non working hours.
  5. Once a node is online, it's kept alive for at least 20 minutes.

## AWS, Requires cloud permissions (IAM)

Jenkins Autoscaler utilizes Amazon EC2 Auto Scaling Groups to manage node groups.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "autoscaling:DescribeAutoScalingGroups",
        "autoscaling:DescribeAutoScalingInstances",
        "autoscaling:DescribeScalingActivities",
        "autoscaling:SetDesiredCapacity",
        "autoscaling:TerminateInstanceInAutoScalingGroup",
        "ec2:DescribeInstances"
      ],
      "Resource": ["*"]
    }
  ]
}
```

## GCP, Requires one of the following OAuth scopes

```plain
https://www.googleapis.com/auth/compute
https://www.googleapis.com/auth/cloud-platform
```

## Compile and run

```bash
make install
jas --help # to see all options
```

example how to run in gce

```bash

jas  \
    --backend=gce \
    --gce-project=my-gce-project \
    --gce-region=us-east1 \
    --gce-instance-group-manager=my-jenkins-instance-group \
    --jenkins-url=https://ci.jenkins.com \
    --jenkins-user=jenkins-admin \
    --jenkins-token=jenkins6user6token6here
```

example how to run in aws

```bash

jas  \
    --backend=aws \
    --aws-region=us-east-1 \
    --aws-autoscaling-group-name=my-jenkins-asg \
    --jenkins-url=https://ci.jenkins.com \
    --jenkins-user=jenkins-admin \
    --jenkins-token=jenkins6user6token6here
```

example how to run with the [config file](/examples/config.example.yml)

```yaml
scaler:
  log_level: debug
  dry_run: true
  run_interval: 3s
  gc_run_interval: 3s
  scale_up_grace_period: 5m
  scale_down_grace_period: 10m
  scale_down_grace_period_during_working_hours: 1h
  controller_node_name: Built-In Node
  scale_up_threshold: 70
  scale_down_threshold: 30
  max_nodes: 10
  min_nodes_during_working_hours: 2
  jenkins_url: https://my.jenkins.co
  jenkins_user: ""
  jenkins_token: ""

backend:
  gce:
    project: gcp-project
    region: us-east1
    instance_group_manager: jenkins-nodes
  aws:
    autoscaling_group_name: jenkins-nodes
    region: us-east-1
```

```bash
# run with configuration file
jas --backend=gce -c ./configs/my-jenkins-autoscaler.yaml
```

## Contribution

Feel free to open Pull-Request for small fixes and changes. For bigger changes and new backends please open an issue first to prevent double work and discuss relevant stuff.

## License

-------

This is free software under the terms of the MIT the license (check the
[LICENSE file](/LICENSE) included in this package).
