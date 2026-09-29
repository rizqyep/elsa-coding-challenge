-- Schema for question sets, quizzes, answer history, and final results (TRD §5.1).

-- +goose Up
CREATE TABLE question_sets (
    id          text PRIMARY KEY,
    title       text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE questions (
    id                 text PRIMARY KEY,
    set_id             text NOT NULL REFERENCES question_sets(id),
    position           int  NOT NULL CHECK (position >= 0),
    prompt             text NOT NULL,
    -- Must reference one of this question's own options. Not a foreign key (it would be
    -- circular); checked by the seed-validation test and by the question-set loader.
    correct_option_id  text NOT NULL,
    UNIQUE (set_id, position)
);

CREATE TABLE options (
    id           text PRIMARY KEY,
    question_id  text NOT NULL REFERENCES questions(id),
    position     int  NOT NULL CHECK (position >= 0),
    text         text NOT NULL,
    UNIQUE (question_id, position)
);

CREATE TABLE quizzes (
    code             char(6) PRIMARY KEY,   -- D16: unique for good
    question_set_id  text NOT NULL REFERENCES question_sets(id),
    host_id          text NOT NULL,
    status           text NOT NULL CHECK (status IN ('lobby', 'running', 'finished', 'expired')),
    window_ms        int  NOT NULL,
    reveal_ms        int  NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    finished_at      timestamptz
);

CREATE TABLE answers (
    quiz_code       char(6) NOT NULL REFERENCES quizzes(code),
    question_id     text    NOT NULL REFERENCES questions(id),
    participant_id  text    NOT NULL,
    option_id       text    NOT NULL,
    correct         boolean NOT NULL,
    points          int     NOT NULL CHECK (points BETWEEN 0 AND 200),
    received_at     timestamptz NOT NULL,
    PRIMARY KEY (quiz_code, question_id, participant_id)   -- makes flushes idempotent
);

CREATE TABLE quiz_results (
    quiz_code       char(6) NOT NULL REFERENCES quizzes(code),
    participant_id  text    NOT NULL,
    display_name    text    NOT NULL,
    total_score     int     NOT NULL CHECK (total_score >= 0),
    rank            int     NOT NULL CHECK (rank >= 1),
    PRIMARY KEY (quiz_code, participant_id)
);
CREATE INDEX quiz_results_by_rank ON quiz_results (quiz_code, rank);

-- +goose Down
DROP TABLE quiz_results;
DROP TABLE answers;
DROP TABLE quizzes;
DROP TABLE options;
DROP TABLE questions;
DROP TABLE question_sets;
