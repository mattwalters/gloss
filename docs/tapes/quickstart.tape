# Writ quickstart terminal cast
# Rendered by `make casts` into mp4 and webm for the docs site.
Source docs/tapes/house.tape

Output ".demo/out/quickstart.mp4"
Output ".demo/out/quickstart.webm"

Hide
Type "source docs/tapes/setup.sh"
Enter
Sleep 1s
Show

# Step 1: Initialize writ
Type "writ init"
Sleep 500ms
Enter
Sleep 2s

# Step 2: Create feature branch and commit
Type "git checkout -b feature"
Sleep 300ms
Enter
Sleep 1s

Type 'echo "package main" > main.go && git add main.go && git commit -m "Add main entry point"'
Sleep 500ms
Enter
Sleep 1500ms

# Step 3: Open a code review
Type 'writ review open -title "Add main entry point" -base main -head feature'
Sleep 500ms
Enter
Sleep 2s

# Step 4: Add a comment
Type 'writ review comment 01918a3b5c6d7e8f90123456789abcde -m "Looks great, ready for review."'
Sleep 500ms
Enter
Sleep 2s

# Step 5: Record approval verdict
Type 'writ review approve 01918a3b5c6d7e8f90123456789abcde -verdict approve -m "LGTM"'
Sleep 500ms
Enter
Sleep 2s

# Step 6: Sync review operations with remote
Type "writ sync origin"
Sleep 500ms
Enter
Sleep 2s

# Step 7: Collaborator inspects reviews in their clone
Type "cd ../collab && writ sync origin >/dev/null 2>&1"
Sleep 300ms
Enter
Sleep 1s

Type "writ review list"
Sleep 500ms
Enter
Sleep 2s

Type "writ review status 01918a3b"
Sleep 500ms
Enter
Sleep 2s

Screenshot ".demo/out/quickstart.png"
Sleep 2s
