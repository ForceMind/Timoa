package ledger

// calendar.go: 月日历视图数据：真实账单与周期事项（含信用卡还款日）
// 分开展示，预测不等于入账。

type CalendarDay struct {
	Date         string              `json:"date"`
	ExpenseCents string              `json:"expense_cents"`
	IncomeCents  string              `json:"income_cents"`
	Events       []CalendarEvent     `json:"events,omitempty"`
}

type CalendarEvent struct {
	Kind   string `json:"kind"` // instance | tx
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
	Amount string `json:"amount_cents,omitempty"`
	RefID  string `json:"ref_id,omitempty"`
}

func (s *Service) CalendarMonth(ledgerID, month string) ([]CalendarDay, error) {
	from := month + "-01"
	to := nextMonth(month)

	days, err := s.DailySums(ledgerID, from, to)
	if err != nil {
		return nil, err
	}
	byDate := map[string]*CalendarDay{}
	out := []CalendarDay{}
	for _, d := range days {
		out = append(out, CalendarDay{Date: d.Date, ExpenseCents: d.ExpenseCents, IncomeCents: d.IncomeCents})
		byDate[d.Date] = &out[len(out)-1]
	}

	// 本月周期事项（含已确认与待确认，状态明示）
	rows, err := s.db.Query(`SELECT i.id,r.name,i.planned_date,i.status,COALESCE(i.planned_amount_cents,0)
		FROM recurrence_instances i JOIN recurrence_rules r ON r.id=i.rule_id
		WHERE i.ledger_id=? AND i.planned_date>=? AND i.planned_date<? AND r.enabled=1
		ORDER BY i.planned_date`, ledgerID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, date, status string
		var planned int64
		if err := rows.Scan(&id, &name, &date, &status, &planned); err != nil {
			return nil, err
		}
		ev := CalendarEvent{Kind: "instance", Name: name, Status: status, RefID: id}
		if planned > 0 {
			ev.Amount = i64s(planned)
		}
		d, ok := byDate[date]
		if !ok {
			out = append(out, CalendarDay{Date: date, ExpenseCents: "0", IncomeCents: "0"})
			d = &out[len(out)-1]
			byDate[date] = d
		}
		d.Events = append(d.Events, ev)
	}

	// 储值卡到期提醒（本月到期的卡进日历事件）
	svRows, err := s.db.Query(`SELECT id,name,expires_on FROM accounts
		WHERE ledger_id=? AND type='stored_value' AND expires_on IS NOT NULL
		AND expires_on>=? AND expires_on<? AND archived_at IS NULL`, ledgerID, from, to)
	if err != nil {
		return nil, err
	}
	defer svRows.Close()
	for svRows.Next() {
		var id, name, exp string
		if err := svRows.Scan(&id, &name, &exp); err != nil {
			return nil, err
		}
		d, ok := byDate[exp]
		if !ok {
			out = append(out, CalendarDay{Date: exp, ExpenseCents: "0", IncomeCents: "0"})
			d = &out[len(out)-1]
			byDate[exp] = d
		}
		d.Events = append(d.Events, CalendarEvent{Kind: "stored_value_expiry", Name: name + " 到期", Status: "expiring", RefID: id})
	}
	return out, nil
}
