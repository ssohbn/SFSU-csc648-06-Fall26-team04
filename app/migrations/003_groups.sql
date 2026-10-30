-- Mode decides who can see and join a group, see policy.go
ALTER TABLE StudyGroups ADD COLUMN Mode TEXT NOT NULL DEFAULT 'OPEN'
    CHECK (Mode IN ('OPEN', 'ACCOUNT', 'INVITE', 'INTEREST', 'SCHOOL'));
ALTER TABLE StudyGroups ADD COLUMN SchoolDomain TEXT NOT NULL DEFAULT '';
ALTER TABLE StudyGroups ADD COLUMN InviteCode TEXT;
ALTER TABLE StudyGroups ADD COLUMN MeetingPlace TEXT NOT NULL DEFAULT '';
ALTER TABLE StudyGroups ADD COLUMN MeetingTime TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX StudyGroupsByInviteCode ON StudyGroups(InviteCode);

CREATE TABLE StudyGroupEnrollment (
    UserID INTEGER NOT NULL REFERENCES Users(Id) ON DELETE CASCADE,
    StudyGroupID INTEGER NOT NULL REFERENCES StudyGroups(Id) ON DELETE CASCADE,
    MemberType TEXT NOT NULL DEFAULT 'USER' CHECK (MemberType IN ('USER', 'ADMIN')),
    PRIMARY KEY (UserID, StudyGroupID)
);
CREATE INDEX StudyGroupEnrollmentByGroup ON StudyGroupEnrollment(StudyGroupID);

CREATE TABLE StudyGroupInterests (
    StudyGroupID INTEGER NOT NULL REFERENCES StudyGroups(Id) ON DELETE CASCADE,
    InterestID INTEGER NOT NULL REFERENCES Interests(Id) ON DELETE CASCADE,
    PRIMARY KEY (StudyGroupID, InterestID)
);
