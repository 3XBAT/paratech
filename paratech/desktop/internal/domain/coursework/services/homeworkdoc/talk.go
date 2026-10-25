package homeworkdoc

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
)

// Fixed wording of a presentation and its speaker text (TASK-106).
const (
	talkHeading      = "Текст выступления"
	talkBySubject    = "Текст выступления к презентации по дисциплине «%s»"
	presentationFile = "Презентация"
	talkFile         = "Текст выступления"
	slideHeading     = "Слайд "
	subjectLabel     = "Дисциплина: "

	// QuestionsHeading opens the likely questions of the speaker text.
	QuestionsHeading = "Вероятные вопросы"
	answerLabel      = "Ответ: "

	// PptxExtension is the extension of a PowerPoint file.
	PptxExtension = ".pptx"
)

// TalkQuestion is a likely question with its short answer.
type TalkQuestion struct {
	Question string
	Answer   string
}

// TalkInput is everything a presentation and its speaker text are built from: the title fields as in Input, the topic,
// the slides in order with their speaker notes (Slide.Notes, one paragraph per line) and the likely questions. Every
// text but the profile is model output.
type TalkInput struct {
	Student   Student
	Subject   string
	Teacher   string
	Topic     string
	Year      int
	Template  values.PresentationTemplate
	Slides    []values.Slide
	Questions []TalkQuestion
}

// BuildPresentation returns the slides as the .pptx shows them: the subtitle of the title slide is the student, the
// subject, the teacher and the place and year from the profile and the lesson (never model text), as many of these
// lines as fit values.MaxSlideSubtitleRunes. Check it with values.Presentation.Validate before rendering.
func BuildPresentation(in TalkInput) values.Presentation {
	p := values.Presentation{Template: in.Template, Slides: make([]values.Slide, 0, len(in.Slides))}
	for _, s := range in.Slides {
		if s.Kind == values.SlideTitle {
			s.Subtitle = subtitle(in)
		}
		p.Slides = append(p.Slides, s)
	}
	return p
}

// subtitle is the lines of the title slide that fit its limits.
func subtitle(in TalkInput) string {
	st := in.Student
	lines := []string{performer(values.CleanName(st.Group), values.CleanName(st.FullName))}
	if subject := values.CleanName(in.Subject); subject != "" {
		lines = append(lines, subjectLabel+subject)
	}
	if teacher := values.CleanName(in.Teacher); teacher != "" {
		lines = append(lines, checkerLabel+teacher)
	}
	lines = append(lines, place(values.CleanName(st.City), in.Year))
	var out []string
	runes := 0
	for _, l := range lines {
		n := utf8.RuneCountInString(l) + 1
		if l == "" || len(out) == values.MaxSlideSubtitleLines || runes+n > values.MaxSlideSubtitleRunes {
			continue
		}
		out, runes = append(out, l), runes+n
	}
	return strings.Join(out, "\n")
}

// BuildTalk returns the speaker text of a presentation: the title page, the mark GeneratedMark, the notes of every
// slide under «Слайд N. <title>», then the likely questions with their answers. Texts are plain text: one-line texts
// are cleaned of control and format characters; the renderer escapes them.
func BuildTalk(in TalkInput) values.Document {
	st := in.Student
	t := values.TitlePage{
		University: values.CleanName(st.University),
		Faculty:    values.CleanName(st.Faculty),
		Heading:    talkHeading,
		Performer:  performer(values.CleanName(st.Group), values.CleanName(st.FullName)),
		Place:      place(values.CleanName(st.City), in.Year),
	}
	if subject := values.CleanName(in.Subject); subject != "" {
		t.Heading = strings.Replace(talkBySubject, "%s", subject, 1)
	}
	if topic := values.CleanName(in.Topic); topic != "" {
		t.Topic = strings.Replace(paperTopicLabel, "%s", topic, 1)
	}
	if teacher := values.CleanName(in.Teacher); teacher != "" {
		t.Checker = checkerLabel + teacher
	}

	doc := values.Document{Title: t}
	doc.Blocks = append(doc.Blocks, paragraph(GeneratedMark))
	for i, s := range in.Slides {
		heading := slideHeading + strconv.Itoa(i+1) + "."
		if title := oneLine(values.CleanPresentationText(s.Title)); title != "" {
			heading += " " + title
		}
		doc.Blocks = append(doc.Blocks, values.Block{Kind: values.BlockHeading, Level: values.MinHeadingLevel, Lines: []string{heading}})
		for _, p := range strings.Split(s.Notes, "\n") {
			if line := oneLine(p); line != "" {
				doc.Blocks = append(doc.Blocks, paragraph(line))
			}
		}
	}
	if len(in.Questions) > 0 {
		doc.Blocks = append(doc.Blocks, values.Block{Kind: values.BlockHeading, Level: values.MinHeadingLevel, Lines: []string{QuestionsHeading}})
		for i, q := range in.Questions {
			question, answer := oneLine(q.Question), oneLine(q.Answer)
			if question == "" {
				continue
			}
			doc.Blocks = append(doc.Blocks,
				values.Block{Kind: values.BlockHeading, Level: values.MinHeadingLevel + 1, Lines: []string{strconv.Itoa(i+1) + ". " + question}},
				paragraph(answerLabel+answer))
		}
	}
	return doc
}

// PresentationFileName is the name of the .pptx of a presentation: «Презентация <topic>», safe for Windows.
func PresentationFileName(topic string) string {
	return strings.TrimSuffix(FileName(strings.TrimSpace(presentationFile+" "+values.CleanName(topic))), DocxExtension) + PptxExtension
}

// TalkFileName is the name of the speaker text: «Текст выступления <topic>», safe for Windows, with DocxExtension.
func TalkFileName(topic string) string {
	return FileName(strings.TrimSpace(talkFile + " " + values.CleanName(topic)))
}
