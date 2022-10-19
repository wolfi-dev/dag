# Running builds on Kubernetes

Prerequisites:
- a Kubernetes cluster (KinD, GKE)
- a container image repo, which you set to the `REPO` env var (e.g., `REPO=gcr.io/jason-chainguard/dag`)

If running on KinD the image has to be publicly-readable.
If running on GKE the image has to be in GCR/AR, in the same project as the cluster.

## Getting Started

Create a pod that runs the build for `brotli` and everything that depends on it:

```
kubectl create -f <(dag pod --repo=$REPO brotli)
```

This will print a unique generated pod name, e.g., `pod/dag-8vjgr created`

Watch for it to transition to a `Running` state:

```
kubectl get pod dag-8vjgr
```

See logs:

```
kubectl logs dag-8vjgr -f
```

## Workload Identity (GKE)

- https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity

```
export PROJECT=$(gcloud config get-value project)
```

Create a Google Service Account (GSA)

```
gcloud iam service-accounts create build-cluster
```

Grant the GSA permission to write to Google Cloud Storage

```
gcloud projects add-iam-policy-binding $PROJECT \
    --member "serviceAccount:build-cluster@$PROJECT.iam.gserviceaccount.com" \
    --role "roles/storage.admin"
```

Bind the GSA to the Kubernetes Service Account (KSA)

```
gcloud iam service-accounts add-iam-policy-binding \
    build-cluster@$PROJECT.iam.gserviceaccount.com \
    --role roles/iam.workloadIdentityUser \
    --member "serviceAccount:$PROJECT.svc.id.goog[default/default]"
```

Annotate the KSA to tell it which GSA it's bound to.

```
kubectl annotate serviceaccount default \
    iam.gke.io/gcp-service-account=build-cluster@$PROJECT.iam.gserviceaccount.com
```

Now when you run the Pod, it can interact with GCS with the GSA's permissions.

You can change the KSA name with the `--service-account` flag.

## Arm Nodes (GKE)

Create a GKE Autopilot cluster that can run Arm nodes:

```
gcloud container clusters create-auto tmp-cluster --region=us-central1 --release-channel=rapid
```

Then request an arm64 build:

```
kubectl create -f <(dag pod --repo=$REPO --arch=arm64 brotli)
```

See logs:

```
kubectl logs dag-kf8dj -f
```

Cleanup the cluster:

```
gcloud container clusters delete tmp-cluster --region=us-central1
```

- https://cloud.google.com/kubernetes-engine/docs/how-to/autopilot-arm-workloads

## Resource Requests

By default, build pods have 1 CPU and 2 GB or memory.

You can request more, for example `dag pod --cpu=30 --ram=50Gi ...`

If you're using GKE Autopilot, see https://cloud.google.com/kubernetes-engine/docs/concepts/autopilot-resource-requests#min-max-requests for minimum and maximum resource requests.
