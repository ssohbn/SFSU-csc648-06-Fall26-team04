-- where a group meets on the map, NULL until the organizer drops a pin
ALTER TABLE StudyGroups ADD COLUMN Latitude REAL CHECK (Latitude BETWEEN -90 AND 90);
ALTER TABLE StudyGroups ADD COLUMN Longitude REAL CHECK (Longitude BETWEEN -180 AND 180);
