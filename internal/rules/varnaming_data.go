package rules

// The var-naming rule decides everything from these fixed tables, vendored
// from the reference versions the parity contract is pinned to (ansible-lint
// 26.8.0 on ansible-core 2.21.3, python 3.14). They change rarely; when the
// pin moves, re-extract them (docs/design/static-yaml-and-var-naming.md
// records the extraction commands).

// pythonKeywords is python's keyword.kwlist. Soft keywords (match, case,
// type, _) are not keywords for iskeyword() and stay out.
var pythonKeywords = map[string]bool{
	"False": true, "None": true, "True": true, "and": true, "as": true,
	"assert": true, "async": true, "await": true, "break": true, "class": true,
	"continue": true, "def": true, "del": true, "elif": true, "else": true,
	"except": true, "finally": true, "for": true, "from": true, "global": true,
	"if": true, "import": true, "in": true, "is": true, "lambda": true,
	"nonlocal": true, "not": true, "or": true, "pass": true, "raise": true,
	"return": true, "try": true, "while": true, "with": true, "yield": true,
}

// ansibleReservedNames is ansible.vars.reserved.get_reserved_names(): play
// and task keywords plus jinja globals ansible injects.
var ansibleReservedNames = map[string]bool{
	"action": true, "always": true, "any_errors_fatal": true, "args": true,
	"async": true, "async_val": true, "become": true, "become_exe": true,
	"become_flags": true, "become_method": true, "become_user": true,
	"block": true, "changed_when": true, "check_mode": true, "collections": true,
	"connection": true, "cycler": true, "debugger": true, "delay": true,
	"delegate_facts": true, "delegate_to": true, "dict": true, "diff": true,
	"environment": true, "fact_path": true, "failed_when": true,
	"force_handlers": true, "gather_facts": true, "gather_timeout": true,
	"handlers": true, "hosts": true, "ignore_errors": true,
	"ignore_unreachable": true, "joiner": true, "lipsum": true,
	"local_action": true, "lookup": true, "loop": true, "loop_control": true,
	"loop_with": true, "max_fail_percentage": true, "module_defaults": true,
	"name": true, "namespace": true, "no_log": true, "notify": true,
	"now": true, "omit": true, "order": true, "poll": true, "port": true,
	"post_tasks": true, "pre_tasks": true, "q": true, "query": true,
	"range": true, "register": true, "remote_user": true, "rescue": true,
	"retries": true, "roles": true, "run_once": true, "serial": true,
	"strategy": true, "tags": true, "tasks": true, "throttle": true,
	"timeout": true, "undef": true, "until": true, "validate_argspec": true,
	"vars": true, "vars_files": true, "vars_prompt": true, "when": true,
	"with_": true,
}

// readOnlyVarNames is upstream var_naming.py's read_only_names: special
// variables users must not assign to. Connection variables are deliberately
// absent, users tune those.
var readOnlyVarNames = map[string]bool{
	"ansible_check_mode": true, "ansible_collection_name": true,
	"ansible_config_file": true, "ansible_dependent_role_names": true,
	"ansible_diff_mode": true, "ansible_forks": true, "ansible_index_var": true,
	"ansible_inventory_sources": true, "ansible_limit": true,
	"ansible_local": true, "ansible_loop": true, "ansible_loop_var": true,
	"ansible_parent_role_names": true, "ansible_parent_role_paths": true,
	"ansible_play_batch": true, "ansible_play_hosts": true,
	"ansible_play_hosts_all": true, "ansible_play_name": true,
	"ansible_play_role_names": true, "ansible_playbook_python": true,
	"ansible_role_name": true, "ansible_role_names": true,
	"ansible_run_tags": true, "ansible_search_path": true,
	"ansible_skip_tags": true, "ansible_verbosity": true,
	"ansible_version": true, "group_names": true, "groups": true,
	"hostvars": true, "inventory_dir": true, "inventory_file": true,
	"inventory_hostname": true, "inventory_hostname_short": true, "omit": true,
	"play_hosts": true, "playbook_dir": true, "role_name": true,
	"role_names": true, "role_path": true,
}

// allowedSpecialVarNames are special variables users may legitimately set.
var allowedSpecialVarNames = map[string]bool{
	"ansible_facts": true, "ansible_become_user": true,
	"ansible_connection": true, "ansible_host": true,
	"ansible_python_interpreter": true, "ansible_user": true,
	"ansible_remote_tmp": true,
}

// annotationKeys are ansible-lint's internal task annotations, never
// variable names.
var annotationKeys = map[string]bool{
	"__ansible_module__": true, "__ansible_module_original__": true,
	"__file__": true, "__line__": true, "__skipped_rules__": true,
}

// playbookRoleKeywords are the keys of a play's `roles:` entry that are role
// keywords rather than role variables.
var playbookRoleKeywords = map[string]bool{
	"any_errors_fatal": true, "become": true, "become_exe": true,
	"become_flags": true, "become_method": true, "become_user": true,
	"check_mode": true, "collections": true, "connection": true,
	"debugger": true, "delegate_facts": true, "delegate_to": true,
	"diff": true, "environment": true, "ignore_errors": true,
	"ignore_unreachable": true, "module_defaults": true, "name": true,
	"no_log": true, "port": true, "remote_user": true, "role": true,
	"run_once": true, "tags": true, "throttle": true, "timeout": true,
	"vars": true, "when": true,
}
