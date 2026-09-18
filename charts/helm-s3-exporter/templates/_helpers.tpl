{{/*
Chart name (nameOverride-aware, standard helm-create logic).
*/}}
{{- define "s3-exporter.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fully qualified app name (release-scoped, standard helm-create logic).
*/}}
{{- define "s3-exporter.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "s3-exporter.imageTag" -}}
{{- default .Chart.AppVersion .Values.image.tag }}
{{- end }}

{{- define "s3-exporter.image" -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.image.repository (include "s3-exporter.imageTag" .) }}
{{- end }}

{{/*
Common labels. "+" is not allowed in label values, so SemVer build
metadata is sanitized.
*/}}
{{- define "s3-exporter.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "s3-exporter.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ include "s3-exporter.imageTag" . | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels (stable subset — never include version/chart here).
*/}}
{{- define "s3-exporter.selectorLabels" -}}
app.kubernetes.io/name: {{ include "s3-exporter.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
