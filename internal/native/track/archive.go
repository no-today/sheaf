package track

func (p *Page) archiveRequirement(id string, archived bool) {
	sess := p.s
	go func() {
		err := sess.ArchiveRequirement(id, archived)
		p.h.Update(func() {
			if err != nil {
				p.h.Toasts().Error(err.Error())
				return
			}
			p.loadReqs()
			text := p.words().ArchivedToast
			if !archived {
				text = p.words().RestoredToast
			}
			p.h.Toasts().Undo(fill(text, "id", id), func() { p.archiveRequirement(id, !archived) }, "")
		})
	}()
}
