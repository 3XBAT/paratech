package prompts

// JSON Schemas of the answers (draft 2020-12). They are documents for the model, placed in the trusted instructions;
// the Hub contract has no schema field (ADR-0005), so the output is checked locally by the Parse functions, which
// enforce the same rules. schema_test.go keeps the two in step: every limit written here is a constant of output.go.

const summarySchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "summary",
  "type": "object",
  "additionalProperties": false,
  "required": ["summary", "key_points"],
  "properties": {
    "summary": {"type": "string", "minLength": 1, "maxLength": 20000, "description": "Summary of the lesson."},
    "key_points": {
      "type": "array",
      "maxItems": 20,
      "items": {"type": "string", "minLength": 1, "maxLength": 300, "description": "One line."}
    }
  }
}`

const extractSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "extract_homework_deadlines",
  "type": "object",
  "additionalProperties": false,
  "required": ["assignments"],
  "properties": {
    "assignments": {
      "type": "array",
      "maxItems": 20,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["title", "description", "due"],
        "properties": {
          "title": {"type": "string", "minLength": 1, "maxLength": 200, "description": "One line."},
          "description": {"type": "string", "maxLength": 4000},
          "due": {"type": ["string", "null"], "format": "date-time", "description": "RFC 3339 with offset, or null."}
        }
      }
    }
  }
}`

const draftSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "homework_draft",
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "body"],
  "properties": {
    "title": {"type": "string", "minLength": 1, "maxLength": 200, "description": "One line."},
    "body": {"type": "string", "minLength": 1, "maxLength": 16000, "description": "The homework text."}
  }
}`

const lectureTextSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "lecture_text",
  "type": "object",
  "additionalProperties": false,
  "required": ["blocks"],
  "properties": {
    "blocks": {
      "type": "array",
      "maxItems": 40,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["heading", "from", "paragraphs"],
        "properties": {
          "heading": {"type": "string", "minLength": 1, "maxLength": 200, "description": "Short topic of the block, one line."},
          "from": {"type": "integer", "minimum": 0, "description": "Index i of the segment where the block starts."},
          "paragraphs": {
            "type": "array",
            "minItems": 1,
            "maxItems": 40,
            "items": {"type": "string", "minLength": 1, "maxLength": 4000, "description": "One paragraph of the text as said."}
          }
        }
      }
    }
  }
}`
