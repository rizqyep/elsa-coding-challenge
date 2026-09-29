-- Seed question sets for local development and demos (TRD §5.5).
-- Applied only when APP_ENV=local, with its own goose version table.

-- +goose Up
INSERT INTO question_sets (id, title) VALUES ('demo-quick', 'Quick demo');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('dq-01', 'demo-quick', 0, 'Choose the synonym of ''rapid''', 'dq-01-b');
INSERT INTO options (id, question_id, position, text) VALUES ('dq-01-a', 'dq-01', 0, 'slow'), ('dq-01-b', 'dq-01', 1, 'quick'), ('dq-01-c', 'dq-01', 2, 'quiet'), ('dq-01-d', 'dq-01', 3, 'heavy');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('dq-02', 'demo-quick', 1, 'What does ''reluctant'' mean?', 'dq-02-b');
INSERT INTO options (id, question_id, position, text) VALUES ('dq-02-a', 'dq-02', 0, 'eager'), ('dq-02-b', 'dq-02', 1, 'unwilling'), ('dq-02-c', 'dq-02', 2, 'curious'), ('dq-02-d', 'dq-02', 3, 'grateful');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('dq-03', 'demo-quick', 2, 'Choose the antonym of ''ancient''', 'dq-03-b');
INSERT INTO options (id, question_id, position, text) VALUES ('dq-03-a', 'dq-03', 0, 'old'), ('dq-03-b', 'dq-03', 1, 'modern'), ('dq-03-c', 'dq-03', 2, 'broken'), ('dq-03-d', 'dq-03', 3, 'famous');

INSERT INTO question_sets (id, title) VALUES ('synonyms-everyday', 'Everyday synonyms');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('syn-01', 'synonyms-everyday', 0, 'Choose the synonym of ''happy''', 'syn-01-a');
INSERT INTO options (id, question_id, position, text) VALUES ('syn-01-a', 'syn-01', 0, 'joyful'), ('syn-01-b', 'syn-01', 1, 'tired'), ('syn-01-c', 'syn-01', 2, 'angry'), ('syn-01-d', 'syn-01', 3, 'nervous');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('syn-02', 'synonyms-everyday', 1, 'Choose the synonym of ''begin''', 'syn-02-b');
INSERT INTO options (id, question_id, position, text) VALUES ('syn-02-a', 'syn-02', 0, 'finish'), ('syn-02-b', 'syn-02', 1, 'start'), ('syn-02-c', 'syn-02', 2, 'wait'), ('syn-02-d', 'syn-02', 3, 'forget');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('syn-03', 'synonyms-everyday', 2, 'Choose the synonym of ''huge''', 'syn-03-b');
INSERT INTO options (id, question_id, position, text) VALUES ('syn-03-a', 'syn-03', 0, 'tiny'), ('syn-03-b', 'syn-03', 1, 'enormous'), ('syn-03-c', 'syn-03', 2, 'narrow'), ('syn-03-d', 'syn-03', 3, 'gentle');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('syn-04', 'synonyms-everyday', 3, 'Choose the synonym of ''difficult''', 'syn-04-b');
INSERT INTO options (id, question_id, position, text) VALUES ('syn-04-a', 'syn-04', 0, 'simple'), ('syn-04-b', 'syn-04', 1, 'hard'), ('syn-04-c', 'syn-04', 2, 'early'), ('syn-04-d', 'syn-04', 3, 'bright');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('syn-05', 'synonyms-everyday', 4, 'Choose the synonym of ''purchase''', 'syn-05-b');
INSERT INTO options (id, question_id, position, text) VALUES ('syn-05-a', 'syn-05', 0, 'sell'), ('syn-05-b', 'syn-05', 1, 'buy'), ('syn-05-c', 'syn-05', 2, 'borrow'), ('syn-05-d', 'syn-05', 3, 'lend');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('syn-06', 'synonyms-everyday', 5, 'Choose the synonym of ''silent''', 'syn-06-a');
INSERT INTO options (id, question_id, position, text) VALUES ('syn-06-a', 'syn-06', 0, 'quiet'), ('syn-06-b', 'syn-06', 1, 'loud'), ('syn-06-c', 'syn-06', 2, 'busy'), ('syn-06-d', 'syn-06', 3, 'bright');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('syn-07', 'synonyms-everyday', 6, 'Choose the synonym of ''brave''', 'syn-07-b');
INSERT INTO options (id, question_id, position, text) VALUES ('syn-07-a', 'syn-07', 0, 'fearful'), ('syn-07-b', 'syn-07', 1, 'courageous'), ('syn-07-c', 'syn-07', 2, 'lazy'), ('syn-07-d', 'syn-07', 3, 'polite');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('syn-08', 'synonyms-everyday', 7, 'Choose the synonym of ''assist''', 'syn-08-a');
INSERT INTO options (id, question_id, position, text) VALUES ('syn-08-a', 'syn-08', 0, 'help'), ('syn-08-b', 'syn-08', 1, 'hinder'), ('syn-08-c', 'syn-08', 2, 'ignore'), ('syn-08-d', 'syn-08', 3, 'follow');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('syn-09', 'synonyms-everyday', 8, 'Choose the synonym of ''wealthy''', 'syn-09-a');
INSERT INTO options (id, question_id, position, text) VALUES ('syn-09-a', 'syn-09', 0, 'rich'), ('syn-09-b', 'syn-09', 1, 'poor'), ('syn-09-c', 'syn-09', 2, 'famous'), ('syn-09-d', 'syn-09', 3, 'careful');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('syn-10', 'synonyms-everyday', 9, 'Choose the synonym of ''mistake''', 'syn-10-a');
INSERT INTO options (id, question_id, position, text) VALUES ('syn-10-a', 'syn-10', 0, 'error'), ('syn-10-b', 'syn-10', 1, 'answer'), ('syn-10-c', 'syn-10', 2, 'promise'), ('syn-10-d', 'syn-10', 3, 'reason');

INSERT INTO question_sets (id, title) VALUES ('business-english', 'Business English');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('biz-01', 'business-english', 0, 'A ''deadline'' is…', 'biz-01-a');
INSERT INTO options (id, question_id, position, text) VALUES ('biz-01-a', 'biz-01', 0, 'the latest time something must be done'), ('biz-01-b', 'biz-01', 1, 'a line in a contract'), ('biz-01-c', 'biz-01', 2, 'a meeting agenda'), ('biz-01-d', 'biz-01', 3, 'a type of invoice');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('biz-02', 'business-english', 1, 'To ''postpone'' a meeting means to…', 'biz-02-b');
INSERT INTO options (id, question_id, position, text) VALUES ('biz-02-a', 'biz-02', 0, 'cancel it'), ('biz-02-b', 'biz-02', 1, 'move it to a later time'), ('biz-02-c', 'biz-02', 2, 'move it to an earlier time'), ('biz-02-d', 'biz-02', 3, 'record it');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('biz-03', 'business-english', 2, '''Revenue'' is…', 'biz-03-a');
INSERT INTO options (id, question_id, position, text) VALUES ('biz-03-a', 'biz-03', 0, 'money a company receives from sales'), ('biz-03-b', 'biz-03', 1, 'money owed to suppliers'), ('biz-03-c', 'biz-03', 2, 'profit after tax'), ('biz-03-d', 'biz-03', 3, 'a bank loan');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('biz-04', 'business-english', 3, 'An ''invoice'' is…', 'biz-04-a');
INSERT INTO options (id, question_id, position, text) VALUES ('biz-04-a', 'biz-04', 0, 'a request for payment for goods or services'), ('biz-04-b', 'biz-04', 1, 'a job application'), ('biz-04-c', 'biz-04', 2, 'a company policy'), ('biz-04-d', 'biz-04', 3, 'a product warranty');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('biz-05', 'business-english', 4, 'To ''negotiate'' means to…', 'biz-05-a');
INSERT INTO options (id, question_id, position, text) VALUES ('biz-05-a', 'biz-05', 0, 'discuss something to reach an agreement'), ('biz-05-b', 'biz-05', 1, 'sign without reading'), ('biz-05-c', 'biz-05', 2, 'refuse every offer'), ('biz-05-d', 'biz-05', 3, 'announce a decision');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('biz-06', 'business-english', 5, 'A ''stakeholder'' is…', 'biz-06-a');
INSERT INTO options (id, question_id, position, text) VALUES ('biz-06-a', 'biz-06', 0, 'anyone with an interest in a project''s outcome'), ('biz-06-b', 'biz-06', 1, 'only the company''s CEO'), ('biz-06-c', 'biz-06', 2, 'a competitor''s supplier'), ('biz-06-d', 'biz-06', 3, 'a type of share certificate');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('biz-07', 'business-english', 6, '''ASAP'' stands for…', 'biz-07-a');
INSERT INTO options (id, question_id, position, text) VALUES ('biz-07-a', 'biz-07', 0, 'as soon as possible'), ('biz-07-b', 'biz-07', 1, 'always stay as planned'), ('biz-07-c', 'biz-07', 2, 'after sales and purchase'), ('biz-07-d', 'biz-07', 3, 'ask someone about prices');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('biz-08', 'business-english', 7, 'To ''delegate'' a task means to…', 'biz-08-a');
INSERT INTO options (id, question_id, position, text) VALUES ('biz-08-a', 'biz-08', 0, 'give it to someone else to do'), ('biz-08-b', 'biz-08', 1, 'finish it early'), ('biz-08-c', 'biz-08', 2, 'reject it'), ('biz-08-d', 'biz-08', 3, 'repeat it');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('biz-09', 'business-english', 8, 'A ''quarter'' in business reporting is…', 'biz-09-a');
INSERT INTO options (id, question_id, position, text) VALUES ('biz-09-a', 'biz-09', 0, 'a three-month period'), ('biz-09-b', 'biz-09', 1, 'a quarter of the staff'), ('biz-09-c', 'biz-09', 2, 'a half-year report'), ('biz-09-d', 'biz-09', 3, 'a weekly target');
INSERT INTO questions (id, set_id, position, prompt, correct_option_id) VALUES ('biz-10', 'business-english', 9, 'Choose the synonym of ''collaborate''', 'biz-10-b');
INSERT INTO options (id, question_id, position, text) VALUES ('biz-10-a', 'biz-10', 0, 'compete'), ('biz-10-b', 'biz-10', 1, 'work together'), ('biz-10-c', 'biz-10', 2, 'resign'), ('biz-10-d', 'biz-10', 3, 'delay');

-- +goose Down
DELETE FROM options WHERE question_id IN (SELECT id FROM questions WHERE set_id IN ('demo-quick', 'synonyms-everyday', 'business-english'));
DELETE FROM questions WHERE set_id IN ('demo-quick', 'synonyms-everyday', 'business-english');
DELETE FROM question_sets WHERE id IN ('demo-quick', 'synonyms-everyday', 'business-english');
