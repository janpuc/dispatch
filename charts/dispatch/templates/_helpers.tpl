{{- define "dispatch.labels" -}}
app.kubernetes.io/name: dispatch-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end }}

{{- define "dispatch.selectorLabels" -}}
app.kubernetes.io/name: dispatch-operator
{{- end }}
