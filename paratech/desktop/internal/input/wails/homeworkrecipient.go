package wails

import (
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/proposal"
)

// The recipient of «Отправить преподавателю» (ADR-0030, TASK-237) as the send dialog (SendPreparation) and the agent's
// confirm card (AgentConfirmCard) give it to the page. JSON fields, the same in both:
//
//	recipient      the address «Кому» shows ("" while the student chooses)
//	recipientName  the teacher's name for «Кому: <имя> <адрес>» ("" when unknown)
//	role           the teacher's role: "lecture", "practice" or "lab"; for reason no_teacher_email the role without an
//	               address ("" when no role has one and the lesson kind is unknown)
//	reason         "choose_teacher": the page asks «Кому отправить?» with options and keeps «Да» disabled until a click;
//	               "no_teacher_email" / "no_subject": the send is refused, message says why («Указать почту» opens
//	               the teacher field of role on the subject page subjectId; no subject offers the subject choice)
//	subjectId      the library subject the teachers come from ("" none)
//	options        [{role, name, email}] for choose_teacher, one per address, in role order
//
// The confirmation carries the clicked option back: ConfirmSend (homework page) and Confirm (agent card) take role
// and the address the option showed ("" and "" when nothing was asked); another current address of that role makes
// the send stale, nothing is sent.

// TeacherOption is one teacher of «Кому отправить?».
type TeacherOption struct {
	Role  string `json:"role"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// ErrSendChooseTeacher is the refusal choose_teacher: «Да» of a send that asks «Кому отправить?» came without a choice.
const ErrSendChooseTeacher = homeworkError("Выберите, кому отправить: лектору, семинаристу или преподавателю лабораторных.")

// Texts of a send refused for its recipient (ADR-0030 B-1).
const (
	msgNoSubject       = "У задания не указан предмет: выберите предмет, и письмо уйдёт его преподавателю."
	msgNoTeacherPrefix = "Нет почты преподавателя"
)

// roleNames name a teacher role in «Нет почты преподавателя (<роль>)».
var roleNames = map[proposal.TeacherRole]string{
	proposal.RoleLecture: "лектор", proposal.RolePractice: "семинарист", proposal.RoleLab: "лабораторные",
}

// recipientMessage explains a send refused for its recipient; subject is the assignment's subject name ("" none). ok
// is false for any other reason.
func recipientMessage(reason proposal.RejectReason, role proposal.TeacherRole, subject string) (msg string, ok bool) {
	switch reason {
	case proposal.ReasonNoSubject:
		return msgNoSubject, true
	case proposal.ReasonNoTeacherEmail:
		msg = msgNoTeacherPrefix
		if name, known := roleNames[role]; known {
			msg += " (" + name + ")"
		}
		if subject != "" {
			msg += " предмета «" + subject + "»"
		}
		return msg + ": укажите её на странице предмета.", true
	}
	return "", false
}

// teacherOptions maps the options of a choice to the page.
func teacherOptions(v homeworkflow.ProposalView) []TeacherOption {
	if len(v.TeacherOptions) == 0 {
		return nil
	}
	out := make([]TeacherOption, 0, len(v.TeacherOptions))
	for _, o := range v.TeacherOptions {
		out = append(out, TeacherOption{Role: string(o.Role), Name: o.Name, Email: o.Email})
	}
	return out
}

// chosenTeacher is the option the student clicked, as the page sends it back.
func chosenTeacher(role, address string) homeworkflow.ChosenTeacher {
	return homeworkflow.ChosenTeacher{Role: proposal.TeacherRole(role), Address: address}
}
