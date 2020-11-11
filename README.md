<p align="center">
	<img src="https://raw.githubusercontent.com/hayorov/helm-bos/master/assets/helm-bos-logo.png" alt="helm-bos logo"/>
</p>

# helm-bos
![Helm3 supported](https://img.shields.io/badge/Helm%203-supported-green)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/dolfly/helm-bos)
[![Build Status](https://travis-ci.org/dolfly/helm-bos.svg?branch=master)](https://travis-ci.org/dolfly/helm-bos)


`helm-bos` is a [helm](https://github.com/kubernetes/helm) plugin that allows you to manage private helm repositories on [Baidu Object Service](https://cloud.baidu.com/doc/BOS/index.html) aka buckets.

## Installation

Install the stable version:

```shell
$ helm plugin install https://github.com/dolfly/helm-bos.git
```

Install a specific version:
```shell
$ helm plugin install https://github.com/dolfly/helm-bos.git --version 1.0.0
```

## Quick start

```shell
# Init a new repository
$ helm bos init bos://bucket/path

# Add your repository to Helm
$ helm repo add repo-name bos://bucket/path

# Push a chart to your repository
$ helm bos push chart.tar.gz repo-name

# Update Helm cache
$ helm repo update

# Fetch the chart
$ helm fetch repo-name/chart

# Remove the chart
$ helm bos rm chart repo-name
```

## Documentation

### Authentification

To authenticate against BOS you can:

use the Global Flag Ak & SK.


### Create a repository

First, you need to [create a bucket on BOS](https://cloud.baidu.com/doc/BOS/s/7k6kqojlr), which will be used by the plugin to store your charts.

Then you have to initialize a repository at a specific location in your bucket:

```shell
$ helm bos init bos://your-bucket/path
```

>   You can create a repository anywhere in your bucket.

>   This command does nothing if a repository already exists at the given location.

You can now add the repository to helm:
```shell
$ helm repo add my-repository bos://your-bucket/path
```

### Push a chart

Package the chart:
```shell
$ helm package my-chart
```
This will create a file `my-chart-<semver>.tgz`.

Now, to push the chart to the repository `my-repository`:

```shell
$ helm bos push my-chart-<semver>.tgz my-repository
```

If you got this error:
```shell
Error: update index file: index is out-of-date
```

That means that someone/something updated the same repository, at the same time as you. You just need to execute the command again or, next time, use the `--retry` flag to automatically retry to push the chart.

Once the chart is uploaded, use helm to fetch it:

```shell
# Update local repo cache if necessary
# $ helm repo update

$ helm fetch my-chart
```

>   This command does nothing if the same chart (name and version) already exists.

>   Using `--retry` is highly recommended in a CI/CD environment.

### Remove a chart

You can remove all the versions of a chart from a repository by running:

```shell
$ helm bos remove my-chart my-repository
```

To remove a specific version, simply use the `--version` flag:

```shell
$ helm bos remove my-chart my-repository --version 0.1.0
```

>   Don't forget to run `helm repo up` after you remove a chart.

## Troubleshootin

You can use the global flag `--debug`, or set `HELM_BOS_DEBUG=true` to get more informations. Please write an issue if you find any bug.

## Helm versions

helm-bos works with Helm 3.