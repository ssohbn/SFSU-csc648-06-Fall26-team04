CREATE TABLE Users (
    Id INTEGER PRIMARY KEY,
    Username TEXT NOT NULL UNIQUE COLLATE NOCASE,
    Email TEXT NOT NULL UNIQUE COLLATE NOCASE,
    PasswordHash TEXT NOT NULL,
    Bio TEXT NOT NULL DEFAULT ''
);

-- only a sha256 of each session token is stored, so a copy of the database
-- can't be used to sign in as anyone
CREATE TABLE Sessions (
    TokenHash BLOB PRIMARY KEY,
    UserID INTEGER NOT NULL REFERENCES Users(Id) ON DELETE CASCADE,
    ExpiresAt INTEGER NOT NULL
);

CREATE TABLE UserInterests (
    UserID INTEGER NOT NULL REFERENCES Users(Id) ON DELETE CASCADE,
    InterestID INTEGER NOT NULL REFERENCES Interests(Id) ON DELETE CASCADE,
    PRIMARY KEY (UserID, InterestID)
);
