{{/*
Helper templates shared across all kcert-checker chart templates.
*/}}

{{/*
Expand the name of the chart.
*/}}
{{- define "kcert-checker.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
If the release name already contains the chart name, avoid repeating it.
*/}}
{{- define "kcert-checker.fullname" -}}
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

{{/*
Common labels applied to every resource.
*/}}
{{- define "kcert-checker.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{ include "kcert-checker.selectorLabels" . }}
app.kubernetes.io/version: {{ .Values.image.tag | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels used by Deployment and Service to match Pods.
*/}}
{{- define "kcert-checker.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kcert-checker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Resolve the ServiceAccount name.
If serviceAccount.create is false, falls back to "default".
*/}}
{{- define "kcert-checker.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "kcert-checker.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
