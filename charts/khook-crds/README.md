# Kagent Hook CRDs Helm Chart

This chart installs the CustomResourceDefinitions (CRDs) required by the Kagent Hook Controller.

## Contents
- `hooks.kagent.dev` CRD

## Install

```bash
# From the repository root
helm install khook-crds ./charts/kagent-hook-crds \
  --namespace kagent \
  --create-namespace
```

Install the controller after CRDs are installed:

```bash
helm install khook-controller ./charts/kagent-hook-controller \
  --namespace kagent \
  --create-namespace
```

## Uninstall

```bash
helm uninstall khook-controller -n kagent
helm uninstall khook-crds -n kagent
```
