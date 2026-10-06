package desktop

type shortcutPlan struct {
	Create bool
	Mark   bool
}

func planDesktopShortcut(ensured, exists, createOK bool) shortcutPlan {
	if ensured {
		return shortcutPlan{}
	}
	if exists {
		return shortcutPlan{Mark: true}
	}
	return shortcutPlan{Create: true, Mark: createOK}
}

type shortcutDeps struct {
	ensured bool
	exists  func() bool
	create  func() error
	mark    func() error
}

func ensureDesktopShortcut(d shortcutDeps) error {
	exists := false
	if d.exists != nil {
		exists = d.exists()
	}
	if d.ensured {
		return nil
	}
	if exists {
		if d.mark != nil {
			return d.mark()
		}
		return nil
	}
	if d.create != nil {
		if err := d.create(); err != nil {
			return err
		}
	}
	if d.mark != nil {
		return d.mark()
	}
	return nil
}
