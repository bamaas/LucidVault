#!/usr/bin/env bash
set -eou pipefail

# work-around wrapper for superset to work correctly.
mise trust
mise install --yes
