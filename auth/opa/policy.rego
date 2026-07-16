package dx.authz

# Default policy: reads data.path_roles, a JSON array of
#   {"method": "POST", "path_pattern": "/iudx/v2/resource_servers", "roles": ["cos_admin"]}
# entries (path_pattern may end in "*" for a prefix match). Denies unless a
# rule's method+path_pattern matches the request and at least one of its
# roles is present in input.roles.

default allow := false

allow if {
	some rule in data.path_roles
	input.method == rule.method
	path_matches(input.path, rule.path_pattern)
	some role in rule.roles
	role in input.roles
}

path_matches(path, pattern) if {
	pattern == path
}

path_matches(path, pattern) if {
	endswith(pattern, "*")
	prefix := trim_suffix(pattern, "*")
	startswith(path, prefix)
}
