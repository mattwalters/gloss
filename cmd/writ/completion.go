package main

import (
	"fmt"
	"io"
	"strings"
)

func runCompletion(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "writ completion: shell required (supported: bash, zsh, fish)")
		return 2
	}
	if len(args) > 1 {
		fmt.Fprintf(stderr, "writ completion: unexpected arguments: %s\n", strings.Join(args[1:], " "))
		return 2
	}

	switch args[0] {
	case "bash":
		emitBashCompletion(stdout)
		return 0
	case "zsh":
		emitZshCompletion(stdout)
		return 0
	case "fish":
		emitFishCompletion(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "writ completion: unsupported shell %q (supported: bash, zsh, fish)\n", args[0])
		return 2
	}
}

func emitBashCompletion(w io.Writer) {
	fmt.Fprint(w, `#!/usr/bin/env bash
# bash completion for writ

_writ() {
    local cur prev words cword
    if declare -F _init_completion >/dev/null 2>&1; then
        _init_completion -n : || return
    else
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
        words=("${COMP_WORDS[@]}")
        cword=$COMP_CWORD
    fi

    local cmd=""
    local subcmd=""
    local i=1

    while [[ $i -lt $cword ]]; do
        local w="${words[i]}"
        case "$w" in
            -C)
                ((i++))
                ;;
            -*)
                ;;
            *)
                if [[ -z "$cmd" ]]; then
                    cmd="$w"
                elif [[ -z "$subcmd" ]]; then
                    subcmd="$w"
                fi
                ;;
        esac
        ((i++))
    done

    # Flag argument completions for closed enums
    case "$prev" in
        -verdict)
            COMPREPLY=($(compgen -W "approve request-changes none" -- "$cur"))
            return 0
            ;;
        -status)
            COMPREPLY=($(compgen -W "draft open closed merged" -- "$cur"))
            return 0
            ;;
        -state)
            COMPREPLY=($(compgen -W "open closed" -- "$cur"))
            return 0
            ;;
        -relation)
            COMPREPLY=($(compgen -W "fixes relates none" -- "$cur"))
            return 0
            ;;
        -sort)
            COMPREPLY=($(compgen -W "created_at_asc created_at_desc updated_at_asc updated_at_desc title_asc title_desc" -- "$cur"))
            return 0
            ;;
    esac

    # Root command completion
    if [[ -z "$cmd" ]]; then
        if [[ "$cur" == -* ]]; then
            COMPREPLY=($(compgen -W "-C -h -help --help" -- "$cur"))
        else
            COMPREPLY=($(compgen -W "init issue review sync completion help" -- "$cur"))
        fi
        return 0
    fi

    case "$cmd" in
        init)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=($(compgen -W "-C -h -help --help" -- "$cur"))
            fi
            ;;
        completion)
            if [[ "$cur" != -* ]]; then
                COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur"))
            fi
            ;;
        help)
            if [[ -z "$subcmd" ]]; then
                COMPREPLY=($(compgen -W "init issue review sync completion help" -- "$cur"))
            else
                case "$subcmd" in
                    review)
                        COMPREPLY=($(compgen -W "open comment approve status list" -- "$cur"))
                        ;;
                    issue)
                        COMPREPLY=($(compgen -W "create status assign list link" -- "$cur"))
                        ;;
                esac
            fi
            ;;
        review)
            if [[ -z "$subcmd" ]]; then
                if [[ "$cur" == -* ]]; then
                    COMPREPLY=($(compgen -W "-C -h -help --help" -- "$cur"))
                else
                    COMPREPLY=($(compgen -W "open comment approve status list" -- "$cur"))
                fi
            else
                case "$subcmd" in
                    open)
                        if [[ "$cur" == -* ]]; then
                            COMPREPLY=($(compgen -W "-C -title -description -base -head -draft -h -help --help" -- "$cur"))
                        fi
                        ;;
                    comment)
                        if [[ "$cur" == -* ]]; then
                            COMPREPLY=($(compgen -W "-C -m -reply-to -h -help --help" -- "$cur"))
                        fi
                        ;;
                    approve)
                        if [[ "$cur" == -* ]]; then
                            COMPREPLY=($(compgen -W "-C -verdict -revision -m -subject -h -help --help" -- "$cur"))
                        fi
                        ;;
                    status)
                        if [[ "$cur" == -* ]]; then
                            COMPREPLY=($(compgen -W "-C -reason -merge-commit --json -json -h -help --help" -- "$cur"))
                        else
                            COMPREPLY=($(compgen -W "draft open closed merged" -- "$cur"))
                        fi
                        ;;
                    list)
                        if [[ "$cur" == -* ]]; then
                            COMPREPLY=($(compgen -W "-C -status -author -text -limit -sort --json -json -h -help --help" -- "$cur"))
                        fi
                        ;;
                esac
            fi
            ;;
        issue)
            if [[ -z "$subcmd" ]]; then
                if [[ "$cur" == -* ]]; then
                    COMPREPLY=($(compgen -W "-C -h -help --help" -- "$cur"))
                else
                    COMPREPLY=($(compgen -W "create status assign list link" -- "$cur"))
                fi
            else
                case "$subcmd" in
                    create)
                        if [[ "$cur" == -* ]]; then
                            COMPREPLY=($(compgen -W "-C -title -description -state -fixes -relates -h -help --help" -- "$cur"))
                        fi
                        ;;
                    status)
                        if [[ "$cur" == -* ]]; then
                            COMPREPLY=($(compgen -W "-C -reason --json -json -h -help --help" -- "$cur"))
                        else
                            COMPREPLY=($(compgen -W "open closed" -- "$cur"))
                        fi
                        ;;
                    assign)
                        if [[ "$cur" == -* ]]; then
                            COMPREPLY=($(compgen -W "-C -add -remove -h -help --help" -- "$cur"))
                        fi
                        ;;
                    list)
                        if [[ "$cur" == -* ]]; then
                            COMPREPLY=($(compgen -W "-C -state -assignee -label -author -text -limit -sort --json -json -h -help --help" -- "$cur"))
                        fi
                        ;;
                    link)
                        if [[ "$cur" == -* ]]; then
                            COMPREPLY=($(compgen -W "-C -target -relation -target-type -h -help --help" -- "$cur"))
                        fi
                        ;;
                esac
            fi
            ;;
        sync)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=($(compgen -W "-C --status -status --json -json -h -help --help" -- "$cur"))
            fi
            ;;
    esac

    return 0
}

complete -F _writ writ
`)
}

func emitZshCompletion(w io.Writer) {
	fmt.Fprint(w, `#compdef writ

_writ() {
    local -a commands
    local -a common_args

    common_args=(
        '-C[Run as if writ was started in <dir>]:dir:_files -/'
        '(-h -help --help)'{-h,-help,--help}'[Show help]'
    )

    _arguments -C \
        $common_args \
        '1: :->command' \
        '*:: :->args'

    case $state in
        command)
            commands=(
                'init:Initialize writ configuration (writer ID and remote fetch refspecs)'
                'issue:Manage issues (create, status, assign, list, link)'
                'review:Manage code reviews (open, comment, approve, status, list)'
                'sync:Synchronize operations with git remotes'
                'completion:Generate shell completion scripts'
                'help:Show help for commands'
            )
            _describe -t commands 'writ command' commands
            ;;
        args)
            case $words[1] in
                init)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '*:remote:'
                    ;;
                completion)
                    _arguments \
                        '1:shell:(bash zsh fish)'
                    ;;
                help)
                    _arguments \
                        '1:command:(init issue review sync completion help)' \
                        '2:subcommand:->help_subcmd'
                    case $words[2] in
                        review)
                            _values 'subcommand' open comment approve status list
                            ;;
                        issue)
                            _values 'subcommand' create status assign list link
                            ;;
                    esac
                    ;;
                review)
                    _writ_review
                    ;;
                issue)
                    _writ_issue
                    ;;
                sync)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '(-status --status)'{-status,--status}'[Report unpushed ops count without network transport]' \
                        '(-json --json)'{-json,--json}'[Output result as JSON]' \
                        '*:remote:'
                    ;;
            esac
            ;;
    esac
}

_writ_review() {
    local -a subcommands
    _arguments -C \
        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
        '1: :->subcommand' \
        '*:: :->args'

    case $state in
        subcommand)
            subcommands=(
                'open:Create a new code review'
                'comment:Add a comment to a review'
                'approve:Record a review verdict'
                'status:View or update review status'
                'list:List code reviews'
            )
            _describe -t subcommands 'review subcommand' subcommands
            ;;
        args)
            case $words[1] in
                open)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '-title[Review title (required)]:title:' \
                        '-description[Review description]:description:' \
                        '-base[Base revision commit or ref]:ref:' \
                        '-head[Head revision commit or ref]:ref:' \
                        '-draft[Create review in draft state]'
                    ;;
                comment)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '-m[Comment message text (required)]:text:' \
                        '-reply-to[Comment ID to reply to]:comment-id:' \
                        '1:review-id:'
                    ;;
                approve)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '-verdict[Review verdict: approve, request-changes, or none]:verdict:(approve request-changes none)' \
                        '-revision[Revision commit ref or SHA]:ref:' \
                        '-m[Review verdict message]:msg:' \
                        '-subject[Subject identity]:subject:' \
                        '1:review-id:'
                    ;;
                status)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '-reason[Reason for status change]:reason:' \
                        '-merge-commit[Merge commit ref or SHA]:ref:' \
                        '(-json --json)'{-json,--json}'[Output result as JSON (view mode only)]' \
                        '1:review-id:' \
                        '2:state:(draft open closed merged)'
                    ;;
                list)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '*-status[Filter by review status (repeatable)]:status:(draft open closed merged)' \
                        '*-author[Filter by author name or email (repeatable)]:author:' \
                        '-text[Filter by text match in title or description]:query:' \
                        '-limit[Maximum number of reviews to return]:limit:' \
                        '-sort[Sort order]:order:(created_at_asc created_at_desc updated_at_asc updated_at_desc title_asc title_desc)' \
                        '(-json --json)'{-json,--json}'[Output result as JSON]'
                    ;;
            esac
            ;;
    esac
}

_writ_issue() {
    local -a subcommands
    _arguments -C \
        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
        '1: :->subcommand' \
        '*:: :->args'

    case $state in
        subcommand)
            subcommands=(
                'create:Create a new issue'
                'status:View or update issue status'
                'assign:Add or remove issue assignees'
                'list:List issues'
                'link:Manage issue cross-reference links'
            )
            _describe -t subcommands 'issue subcommand' subcommands
            ;;
        args)
            case $words[1] in
                create)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '-title[Issue title (required)]:title:' \
                        '-description[Issue description]:description:' \
                        '-state[Initial issue state (open or closed)]:state:(open closed)' \
                        '*-fixes[Add a fixes cross-reference link (repeatable)]:ref:' \
                        '*-relates[Add a relates cross-reference link (repeatable)]:ref:'
                    ;;
                status)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '-reason[Reason for status change]:reason:' \
                        '(-json --json)'{-json,--json}'[Output result as JSON (view mode only)]' \
                        '1:issue-id:' \
                        '2:state:(open closed)'
                    ;;
                assign)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '*-add[Add assignee email or ID (repeatable)]:assignee:' \
                        '*-remove[Remove assignee email or ID (repeatable)]:assignee:' \
                        '1:issue-id:'
                    ;;
                list)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '*-state[Filter by issue state (repeatable)]:state:(open closed)' \
                        '*-assignee[Filter by assignee name or email (repeatable)]:assignee:' \
                        '*-label[Filter by label (repeatable)]:label:' \
                        '*-author[Filter by author name or email (repeatable)]:author:' \
                        '-text[Filter by text match in title or description]:query:' \
                        '-limit[Maximum number of issues to return]:limit:' \
                        '-sort[Sort order]:order:(created_at_asc created_at_desc updated_at_asc updated_at_desc title_asc title_desc)' \
                        '(-json --json)'{-json,--json}'[Output result as JSON]'
                    ;;
                link)
                    _arguments \
                        '-C[Run as if writ was started in <dir>]:dir:_files -/' \
                        '-target[Target reference (required)]:ref:' \
                        '-relation[Link relation (required)]:relation:(fixes relates none)' \
                        '-target-type[Target object type]:type:' \
                        '1:issue-id:'
                    ;;
            esac
            ;;
    esac
}

_writ "$@"
`)
}

func emitFishCompletion(w io.Writer) {
	fmt.Fprint(w, `# fish completion for writ

complete -c writ -f

# Global flags
complete -c writ -l C -d "Run as if writ was started in <dir>" -r -a "(__fish_complete_directories)"
complete -c writ -s h -l help -d "Show help"

# Top-level commands
complete -c writ -n "__fish_use_subcommand" -a init -d "Initialize writ configuration (writer ID and remote fetch refspecs)"
complete -c writ -n "__fish_use_subcommand" -a issue -d "Manage issues (create, status, assign, list, link)"
complete -c writ -n "__fish_use_subcommand" -a review -d "Manage code reviews (open, comment, approve, status, list)"
complete -c writ -n "__fish_use_subcommand" -a sync -d "Synchronize operations with git remotes"
complete -c writ -n "__fish_use_subcommand" -a completion -d "Generate shell completion scripts"
complete -c writ -n "__fish_use_subcommand" -a help -d "Show help for commands"

# Subcommands for review
complete -c writ -n "__fish_seen_subcommand_from review; and not __fish_seen_subcommand_from open comment approve status list" -a open -d "Create a new code review"
complete -c writ -n "__fish_seen_subcommand_from review; and not __fish_seen_subcommand_from open comment approve status list" -a comment -d "Add a comment to a review"
complete -c writ -n "__fish_seen_subcommand_from review; and not __fish_seen_subcommand_from open comment approve status list" -a approve -d "Record a review verdict"
complete -c writ -n "__fish_seen_subcommand_from review; and not __fish_seen_subcommand_from open comment approve status list" -a status -d "View or update review status"
complete -c writ -n "__fish_seen_subcommand_from review; and not __fish_seen_subcommand_from open comment approve status list" -a list -d "List code reviews"

# Flags for review open
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from open" -l title -d "Review title (required)" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from open" -l description -d "Review description" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from open" -l base -d "Base revision commit or ref" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from open" -l head -d "Head revision commit or ref" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from open" -l draft -d "Create review in draft state"

# Flags for review comment
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from comment" -l m -d "Comment message text (required)" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from comment" -l reply-to -d "Comment ID to reply to" -r

# Flags for review approve
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from approve" -l verdict -d "Verdict (default: approve)" -r -a "approve request-changes none"
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from approve" -l revision -d "Revision commit ref or SHA" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from approve" -l m -d "Verdict message" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from approve" -l subject -d "Subject identity" -r

# Flags for review status
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from status" -l reason -d "Reason for status change" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from status" -l merge-commit -d "Merge commit ref or SHA" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from status" -l json -d "Output result as JSON (view mode only)"
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from status" -a "draft open closed merged"

# Flags for review list
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from list" -l status -d "Filter by review status (repeatable)" -r -a "draft open closed merged"
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from list" -l author -d "Filter by author name or email (repeatable)" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from list" -l text -d "Filter by text match in title or description" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from list" -l limit -d "Maximum number of reviews to return" -r
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from list" -l sort -d "Sort order" -r -a "created_at_asc created_at_desc updated_at_asc updated_at_desc title_asc title_desc"
complete -c writ -n "__fish_seen_subcommand_from review; and __fish_seen_subcommand_from list" -l json -d "Output result as JSON"

# Subcommands for issue
complete -c writ -n "__fish_seen_subcommand_from issue; and not __fish_seen_subcommand_from create status assign list link" -a create -d "Create a new issue"
complete -c writ -n "__fish_seen_subcommand_from issue; and not __fish_seen_subcommand_from create status assign list link" -a status -d "View or update issue status"
complete -c writ -n "__fish_seen_subcommand_from issue; and not __fish_seen_subcommand_from create status assign list link" -a assign -d "Add or remove issue assignees"
complete -c writ -n "__fish_seen_subcommand_from issue; and not __fish_seen_subcommand_from create status assign list link" -a list -d "List issues"
complete -c writ -n "__fish_seen_subcommand_from issue; and not __fish_seen_subcommand_from create status assign list link" -a link -d "Manage issue cross-reference links"

# Flags for issue create
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from create" -l title -d "Issue title (required)" -r
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from create" -l description -d "Issue description" -r
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from create" -l state -d "Initial issue state (open or closed)" -r -a "open closed"
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from create" -l fixes -d "Add a fixes cross-reference link (repeatable)" -r
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from create" -l relates -d "Add a relates cross-reference link (repeatable)" -r

# Flags for issue status
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from status" -l reason -d "Reason for status change" -r
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from status" -l json -d "Output result as JSON (view mode only)"
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from status" -a "open closed"

# Flags for issue assign
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from assign" -l add -d "Add assignee email or ID (repeatable)" -r
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from assign" -l remove -d "Remove assignee email or ID (repeatable)" -r

# Flags for issue list
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from list" -l state -d "Filter by issue state (repeatable)" -r -a "open closed"
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from list" -l assignee -d "Filter by assignee name or email (repeatable)" -r
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from list" -l label -d "Filter by label (repeatable)" -r
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from list" -l author -d "Filter by author name or email (repeatable)" -r
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from list" -l text -d "Filter by text match in title or description" -r
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from list" -l limit -d "Maximum number of issues to return" -r
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from list" -l sort -d "Sort order" -r -a "created_at_asc created_at_desc updated_at_asc updated_at_desc title_asc title_desc"
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from list" -l json -d "Output result as JSON"

# Flags for issue link
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from link" -l target -d "Target reference" -r
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from link" -l relation -d "Link relation: fixes, relates, or none" -r -a "fixes relates none"
complete -c writ -n "__fish_seen_subcommand_from issue; and __fish_seen_subcommand_from link" -l target-type -d "Target object type" -r

# Flags for sync
complete -c writ -n "__fish_seen_subcommand_from sync" -l status -d "Report unpushed ops count without network transport"
complete -c writ -n "__fish_seen_subcommand_from sync" -l json -d "Output result as JSON"

# Completion command
complete -c writ -n "__fish_seen_subcommand_from completion" -a "bash zsh fish"

# Help command
complete -c writ -n "__fish_seen_subcommand_from help; and not __fish_seen_subcommand_from init issue review sync completion help" -a "init issue review sync completion help"
complete -c writ -n "__fish_seen_subcommand_from help; and __fish_seen_subcommand_from review" -a "open comment approve status list"
complete -c writ -n "__fish_seen_subcommand_from help; and __fish_seen_subcommand_from issue" -a "create status assign list link"
`)
}
