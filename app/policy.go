package main

import "strings"

// Group modes decide who can see and join a group. Members can always see
// the groups they're in.
const (
	modeOpen     = "OPEN"
	modeAccount  = "ACCOUNT"
	modeInvite   = "INVITE"
	modeInterest = "INTEREST"
	modeSchool   = "SCHOOL"
)

type modeChoice struct {
	Value, Label, Help string
}

var modeChoices = []modeChoice{
	{modeOpen, "Open", "Anyone can find this group. Anyone with an account can join."},
	{modeAccount, "Signed-in only", "Only signed-in students can see and join this group."},
	{modeInvite, "Invite only", "Hidden from search. Only people with the invite link can see and join."},
	{modeInterest, "Shared interests", "Anyone can see this group. Joining needs one of its topics on your profile."},
	{modeSchool, "School only", "Only students with an email from this school can see and join."},
}

func modeInfo(mode string) (modeChoice, bool) {
	for _, c := range modeChoices {
		if c.Value == mode {
			return c, true
		}
	}
	return modeChoice{}, false
}

// access is what one viewer brings to one group.
type access struct {
	user        *User // nil for guests
	inviteOK    bool  // sent the group's current invite code
	sharesTopic bool  // has one of the group's topics on their profile
}

func canView(g StudyGroup, a access) bool {
	if g.Role != "" {
		return true
	}
	switch g.Mode {
	case modeOpen, modeInterest:
		return true
	case modeAccount:
		return a.user != nil
	case modeInvite:
		return a.inviteOK
	case modeSchool:
		return a.user != nil && g.SchoolDomain != "" && schoolDomain(a.user.Email) == g.SchoolDomain
	}
	return false
}

// canJoin is for signed-in viewers who aren't members yet.
func canJoin(g StudyGroup, a access) bool {
	return a.user != nil && canView(g, a) && (g.Mode != modeInterest || a.sharesTopic)
}

// schoolDomain is the school part of an .edu email, so "mail.sfsu.edu" and
// "sfsu.edu" both give "sfsu.edu". Other emails give "". Nothing proves the
// person owns the address; that's fine for this version.
func schoolDomain(email string) string {
	labels := strings.Split(strings.ToLower(email[strings.LastIndex(email, "@")+1:]), ".")
	n := len(labels)
	if n < 2 || labels[n-1] != "edu" || labels[n-2] == "" {
		return ""
	}
	return labels[n-2] + ".edu"
}
