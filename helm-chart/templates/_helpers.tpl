{{- define "dancehub.image" -}}
{{- $repository := required "images.repository is required" .root.Values.images.repository -}}
{{- $tag := required "images.tag is required" .root.Values.images.tag -}}
{{- printf "%s/%s:%s" (trimSuffix "/" $repository) .name $tag -}}
{{- end -}}

{{/*
Definition of a caller (service or worker) by name; fails the render for unknown names so a
typo cannot silently produce a policy that admits nobody.
*/}}
{{- define "dancehub.callerDefinition" -}}
{{- $definition := index .root.Values.dancehub.services .caller -}}
{{- if not $definition -}}{{- $definition = index .root.Values.dancehub.workers .caller -}}{{- end -}}
{{- if not $definition -}}{{- fail (printf "dancehub.services.%s.allowedCallers lists unknown caller %s" .name .caller) -}}{{- end -}}
{{- toJson $definition -}}
{{- end -}}

{{/*
MESH_ALLOWED_PEERS value: "sa,sa=principal|principal". Every allowed caller's ServiceAccount,
followed by the service principals that workload is allowed to assert.
*/}}
{{- define "dancehub.meshPeers" -}}
{{- $entries := list -}}
{{- range $caller := .callers -}}
{{- $definition := include "dancehub.callerDefinition" (dict "root" $.root "name" $.name "caller" $caller) | fromJson -}}
{{- $principals := default (list) $definition.servicePrincipals -}}
{{- if $principals -}}
{{- $entries = append $entries (printf "%s=%s" $caller (join "|" $principals)) -}}
{{- else -}}
{{- $entries = append $entries $caller -}}
{{- end -}}
{{- end -}}
{{- join "," $entries -}}
{{- end -}}

{{/* SPIFFE principal of a workload in this release, as used by AuthorizationPolicy. */}}
{{- define "dancehub.meshPrincipal" -}}
{{- printf "%s/ns/%s/sa/%s" .root.Values.dancehub.serviceMesh.trustDomain .root.Release.Namespace .name -}}
{{- end -}}

{{/* Pod labels that opt the workload into sidecar injection when the mesh is enabled. */}}
{{- define "dancehub.meshPodLabels" -}}
{{- if .root.Values.dancehub.serviceMesh.enabled }}
sidecar.istio.io/inject: {{ .inject | quote }}
{{- with .root.Values.dancehub.serviceMesh.revision }}
istio.io/rev: {{ . | quote }}
{{- end }}
{{- end }}
{{- end -}}
