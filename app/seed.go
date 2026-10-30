package main

import (
	"context"
	"crypto/rand"
	"log"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// seed fills an empty database with demo accounts and groups so the site has
// something to show. It does nothing once anyone has signed up.
func seed(ctx context.Context, s *store) error {
	var users int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM Users`).Scan(&users); err != nil {
		return err
	}
	if users > 0 {
		log.Print("seed: skipped, the database already has accounts")
		return nil
	}

	password := strings.ToLower(rand.Text())
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	people := []struct {
		name, email string
		interests   []string
	}{
		{"maya", "maya.demo@sfsu.edu", []string{"Calculus", "Databases"}},
		{"jordan", "jordan.demo@sfsu.edu", []string{"Data Structures", "Interview Prep"}},
		{"priya", "priya.demo@sfsu.edu", []string{"Machine Learning", "Linear Algebra"}},
		{"leo", "leo.demo@example.com", []string{"Web Development", "Spanish"}},
		{"sam", "sam.demo@example.com", []string{"Physics", "Calculus"}},
	}
	ids := map[string]int64{}
	var names []string
	for _, p := range people {
		id, err := s.createUser(ctx, p.name, p.email, string(hash))
		if err != nil {
			return err
		}
		ids[p.name] = id
		names = append(names, p.name)
		for _, interest := range p.interests {
			if err := s.createUserInterest(ctx, id, interest); err != nil {
				return err
			}
		}
	}

	groups := []struct {
		owner   string
		group   StudyGroup
		topics  []string
		members []string
	}{
		{"maya", StudyGroup{
			Name: "Calc II Problem Nights", ShortDescription: "Work through the weekly problem set together.",
			LongDescription: "Bring the homework and whatever is confusing you. We split into small tables by section.",
			Mode:            modeOpen, MeetingPlace: "J. Paul Leonard Library", MeetingTime: "Tuesdays, 6 pm",
		}, []string{"Calculus"}, []string{"sam", "priya"}},
		{"maya", StudyGroup{
			Name: "CSC 675 SQL Practice", ShortDescription: "Practice queries before each workshop.",
			LongDescription: "We go over the last workshop and try the next one's warmups. SFSU students only.",
			Mode:            modeSchool, SchoolDomain: "sfsu.edu",
			MeetingPlace: "Cesar Chavez Student Center", MeetingTime: "Thursdays, 4 pm",
		}, []string{"Databases"}, []string{"jordan"}},
		{"jordan", StudyGroup{
			Name: "LeetCode Mornings", ShortDescription: "Two timed problems, then we talk through them.",
			LongDescription: "Good for internship prep. Mediums mostly, one hard on the last Saturday of the month.",
			Mode:            modeAccount, MeetingPlace: "Philz Coffee, 24th Street", MeetingTime: "Saturdays, 9 am",
		}, []string{"Interview Prep", "Data Structures"}, []string{"priya", "leo"}},
		{"priya", StudyGroup{
			Name: "ML Paper Club", ShortDescription: "Read one machine learning paper a week and discuss it.",
			LongDescription: "Papers are picked a week ahead. Come with one question about the paper.",
			Mode:            modeInterest, MeetingPlace: "San Francisco Public Library, Main", MeetingTime: "Every other Sunday, 2 pm",
		}, []string{"Machine Learning", "Linear Algebra"}, []string{"maya"}},
		{"jordan", StudyGroup{
			Name: "Capstone Team Standup", ShortDescription: "Weekly check-in for our CSC 648 team.",
			LongDescription: "Demo what you finished, plan the next milestone, then pair on whatever is stuck.",
			Mode:            modeInvite, MeetingPlace: "Mechanics' Institute Library", MeetingTime: "Mondays, 7 pm",
		}, []string{"Software Engineering"}, []string{"maya"}},
		{"sam", StudyGroup{
			Name: "Physics 230 Study Hall", ShortDescription: "Quiet study with people to ask when you're stuck.",
			Mode: modeOpen, MeetingPlace: "Sunset Branch Library", MeetingTime: "Wednesdays, 5 pm",
		}, []string{"Physics", "Calculus"}, nil},
		{"leo", StudyGroup{
			Name: "Web Dev Build Night", ShortDescription: "Bring a side project and build it with others.",
			LongDescription: "Any stack is welcome. Short show and tell at the end.",
			Mode:            modeOpen, MeetingPlace: "Mission Branch Library", MeetingTime: "Fridays, 6 pm",
		}, []string{"Web Development"}, []string{"jordan"}},
		{"leo", StudyGroup{
			Name: "Spanish Conversation Hour", ShortDescription: "Practice speaking Spanish at any level.",
			Mode: modeInterest, MeetingPlace: "de Young Museum", MeetingTime: "Sundays, 11 am",
		}, []string{"Spanish"}, nil},
	}
	for _, g := range groups {
		id, err := s.createGroup(ctx, ids[g.owner], g.group, g.topics)
		if err != nil {
			return err
		}
		for _, m := range g.members {
			if err := s.joinGroup(ctx, ids[m], id); err != nil {
				return err
			}
		}
	}
	log.Printf("seed: added %d groups and demo accounts %s, all with password %s",
		len(groups), strings.Join(names, ", "), password)
	return nil
}
