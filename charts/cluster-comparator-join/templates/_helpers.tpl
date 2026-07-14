{{- define "join.labels" -}}
app.kubernetes.io/name: cluster-comparator-join
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
