# Running builds on Kubernetes

Prerequisites:
- a Kubernetes cluster (KinD, GKE)
- a container image repo, which you set to the `REPO` env var (e.g., `REPO=gcr.io/jason-chainguard/dag`)

If running on KinD the image has to be publicly-readable.
If running on GKE the image has to be in GCR/AR, in the same project as the cluster.

> ⚠️ GKE Autopilot is not currently supported, due to bubblewrap requiring `--privileged`.

To create a suitable GKE cluster:

```
export PROJECT=$(gcloud config get-value project)
```

```
gcloud container clusters create tmp-cluster \
    --zone            us-central1-b  \
    --machine-type    e2-standard-32 \
    --num-nodes       1 \
    --release-channel rapid \
    --workload-pool=${PROJECT}.svc.id.goog
```

## Getting Started

Run pod that executes `make all`:

```
dag pod --repo=$REPO
```

This will create a Pod with a unique generated name to `make all`, watch it until it starts, and tail logs.

If Pod creation or initialization fails, or if the build running in the Pod fails, the command fails.

You can specify a subset of packages to build as positional args, e.g., `dag pod ... brotli git-lfs`

You can pass `--watch=false` to only create the Pod and not watch it.
You can pass `--create=false` to print the Pod YAML but not create it.

By default the Pod is created in the `default` namespace.
Use `--namespace` (`-n`) to change this.

## Workload Identity (GKE)

- https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity


Create a Google Service Account (GSA)

```
gcloud iam service-accounts create build-cluster
```

Grant the GSA permission to write to Google Cloud Storage

```
gcloud projects add-iam-policy-binding ${PROJECT} \
    --member "serviceAccount:build-cluster@${PROJECT}.iam.gserviceaccount.com" \
    --role   roles/storage.admin
```

Bind the GSA to the Kubernetes Service Account (KSA)

```
gcloud iam service-accounts add-iam-policy-binding \
    build-cluster@${PROJECT}.iam.gserviceaccount.com \
    --role    roles/iam.workloadIdentityUser \
    --member "serviceAccount:${PROJECT}.svc.id.goog[default/default]"
```

Annotate the KSA to tell it which GSA it's bound to.

```
kubectl annotate serviceaccount default \
    "iam.gke.io/gcp-service-account=build-cluster@${PROJECT}.iam.gserviceaccount.com"
```

Now when you run the Pod, it can interact with GCS with the GSA's permissions.

You can change the KSA name with the `--service-account` flag -- if you do this, or change `--namespace`, make sure you bind the GSA to the correct KSA, and annotate the KSA!

## Arm Nodes (GKE)

- https://cloud.google.com/kubernetes-engine/docs/how-to/prepare-arm-workloads-for-deployment

_Note: This doesn't currently work to build wolfi, since stage3 packages are not available for arm64 yet._

Add Arm nodes to an existing cluster:

```
gcloud container node-pools create arm-nodes \
    --cluster        tmp-cluster \
    --zone           us-central1-b \
    --machine-type   t2a-standard-32 \
    --num-nodes      1
```

(Arm nodes currently require `us-central1` and a recent Kubernetes version, which you get from the Rapid release channel.)

Then request an arm64 build and see logs:

```
dag pod --repo=$REPO --arch=arm64
```

Cleanup the cluster:

```
gcloud container clusters delete tmp-cluster --region=us-central1
```

## Resource Requests

By default, build pods have 1 CPU and 2 GB or memory.

You can request more, for example `dag pod --cpu=4 --ram=12Gi ...`

Note: Check the nodes you configured for the cluster, to make sure you're not requesting a Pod that won't fit on any nodes.
