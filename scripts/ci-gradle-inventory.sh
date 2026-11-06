#!/usr/bin/env bash
set -euo pipefail

# Restricts Gradle toolchain resolution to the three pinned JDKs. The runner
# image's own JDKs stay in place: setup-java exposes its toolcache entries as
# symlinks into /usr/lib/jvm, so hiding that directory breaks JAVA_HOME.
#
# The pins are major versions, not exact patches: Lucy's toolchain selection
# matches majors only, and Adoptium re-tags LTS releases.

: "${RUNNER_TEMP:?RUNNER_TEMP is not set}"
: "${GITHUB_ENV:?GITHUB_ENV is not set}"
: "${JDK17:?JDK17 is not set}"
: "${JDK21:?JDK21 is not set}"
: "${JDK25:?JDK25 is not set}"

export GRADLE_USER_HOME="${RUNNER_TEMP}/lucy-gradle"
mkdir -p "$GRADLE_USER_HOME"
printf "%s\n" \
  "org.gradle.java.installations.auto-detect=false" \
  "org.gradle.java.installations.auto-download=false" \
  "org.gradle.java.installations.paths=${JDK17},${JDK21},${JDK25}" \
  > "${GRADLE_USER_HOME}/gradle.properties"
printf "GRADLE_USER_HOME=%s\n" "$GRADLE_USER_HOME" >> "$GITHUB_ENV"
