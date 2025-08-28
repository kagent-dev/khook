# Kagent Hook CRDs Helm Chart

This chart installs the CustomResourceDefinitions (CRDs) required by the Kagent Hook Controller.

## Contents
- `hooks.kagent.dev` CRD

## Install

```bash
# From the repository root
helm install kagent-hook-crds ./charts/kagent-hook-crds \
  --namespace kagent \
  --create-namespace
```

Install the controller after CRDs are installed:

```bash
helm install kagent-hook-controller ./charts/kagent-hook-controller \
  --namespace kagent \
  --create-namespace
```

## Uninstall

```bash
helm uninstall kagent-hook-controller -n kagent
helm uninstall kagent-hook-crds -n kagent
```
