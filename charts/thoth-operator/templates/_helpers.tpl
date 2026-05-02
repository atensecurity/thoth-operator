{{- define "thoth-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "thoth-operator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- include "thoth-operator.name" . -}}
{{- end -}}
{{- end -}}

{{- define "thoth-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "thoth-operator.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}
