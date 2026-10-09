import AppKit

private enum TrayMenuItemSublabelStyle: String {
    case badge
    case muted
    case destructive
    case warning
    case secondary
}

private enum TrayMenuItemType: String {
    case action
    case checkbox
    case submenu
    case separator
}

private final class TrayNativeMenuItem: NSMenuItem {
    let trayType: TrayMenuItemType

    init(type: TrayMenuItemType) {
        trayType = type
        super.init(title: "", action: nil, keyEquivalent: "")
    }

    required init(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }
}

private final class TrayMenuHighlightView: NSVisualEffectView {
    override var allowsVibrancy: Bool {
        true
    }

    override func hitTest(_ point: NSPoint) -> NSView? {
        nil
    }
}

private final class TrayMenuDrawingView: NSView {
    var drawHandler: (() -> Void)?

    override var isFlipped: Bool {
        true
    }

    override func hitTest(_ point: NSPoint) -> NSView? {
        nil
    }

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)
        drawHandler?()
    }
}

private final class TrayMenuItemView: NSView {
    private enum Metrics {
        static let height: CGFloat = 24
        static let minimumWidth: CGFloat = 270
        static let maximumWidth: CGFloat = 520
        static let stateImageLeading: CGFloat = 12
        static let stateImageWidth: CGFloat = 12
        static let stateImageHeight: CGFloat = 11
        static let stateImageTitleSpacing: CGFloat = 3
        static let titleTextInset: CGFloat = 2
        static let titleLeading: CGFloat = {
            ProcessInfo.processInfo.operatingSystemVersion.majorVersion >= 26
                ? 14
                : 12
        }()
        static let titleBadgeSpacing: CGFloat = 12
        static let trailing: CGFloat = 16
        static let badgeHeight: CGFloat = 16
        static let badgeHorizontalPadding: CGFloat = 5
        static let submenuIndicatorWidth: CGFloat = 9
        static let submenuIndicatorHeight: CGFloat = 12
        static let submenuColumnSpacing: CGFloat = 8
        static let minimumTitleWidth: CGFloat = 60
        static let highlightHorizontalInset: CGFloat = 5
        static let highlightVerticalInset: CGFloat = 0
        static let highlightCornerRadius: CGFloat = {
            ProcessInfo.processInfo.operatingSystemVersion.majorVersion >= 26
                ? 7
                : 4
        }()
    }

    private var label: String
    private var sublabel: String?
    private var sublabelStyle: TrayMenuItemSublabelStyle
    private var checked: Bool
    private var keepsMenuOpen: Bool
    private var hasSubmenu: Bool
    private var reservesSubmenuColumn = false
    private var reservesStateColumn = false
    private var pointerInside = false
    private var trackingAreaReference: NSTrackingArea?

    private let titleFont = NSFont.menuFont(ofSize: 0)
    private let badgeFont = NSFont.monospacedDigitSystemFont(
        ofSize: NSFont.labelFontSize,
        weight: .medium
    )

    private let highlightView: NSVisualEffectView = {
        let view = TrayMenuHighlightView()
        view.material = NSVisualEffectView.Material(rawValue: 36) ?? .selection
        view.blendingMode = .behindWindow
        view.state = .active
        view.isEmphasized = true
        view.wantsLayer = true
        view.layer?.cornerRadius = Metrics.highlightCornerRadius
        view.layer?.cornerCurve = .continuous
        view.layer?.masksToBounds = true
        view.isHidden = true
        return view
    }()

    private let drawingView = TrayMenuDrawingView()

    private let checkmarkView: NSImageView = {
        let view = NSImageView()
        let configuration = NSImage.SymbolConfiguration(
            pointSize: NSFont.menuFont(ofSize: 0).pointSize,
            weight: .bold,
            scale: .small
        )
        view.image = NSImage(named: NSImage.menuOnStateTemplateName)?
            .withSymbolConfiguration(configuration)
        view.imageAlignment = .alignCenter
        view.imageScaling = .scaleProportionallyDown
        view.isHidden = true
        return view
    }()

    private let submenuIndicatorView: NSImageView = {
        let view = NSImageView()
        let configuration = NSImage.SymbolConfiguration(
            pointSize: NSFont.menuFont(ofSize: 0).pointSize,
            weight: .bold,
            scale: .small
        )
        view.image = NSImage(
            systemSymbolName: "chevron.right",
            accessibilityDescription: nil
        )?.withSymbolConfiguration(configuration)
        view.imageAlignment = .alignCenter
        view.imageScaling = .scaleProportionallyDown
        view.isHidden = true
        return view
    }()

    init(
        label: String,
        sublabel: String?,
        sublabelStyle: TrayMenuItemSublabelStyle,
        checked: Bool,
        keepsMenuOpen: Bool,
        hasSubmenu: Bool
    ) {
        self.label = label
        self.sublabel = sublabel
        self.sublabelStyle = sublabelStyle
        self.checked = checked
        self.keepsMenuOpen = keepsMenuOpen
        self.hasSubmenu = hasSubmenu
        super.init(
            frame: NSRect(
                x: 0,
                y: 0,
                width: Metrics.minimumWidth,
                height: Metrics.height
            )
        )
        autoresizingMask = [.width]
        addSubview(highlightView)
        addSubview(drawingView)
        drawingView.addSubview(checkmarkView)
        drawingView.addSubview(submenuIndicatorView)
        drawingView.drawHandler = { [weak self] in
            self?.drawContent()
        }
        frame.size.width = preferredWidth
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    override var isFlipped: Bool {
        true
    }

    override var intrinsicContentSize: NSSize {
        NSSize(width: preferredWidth, height: Metrics.height)
    }

    override func layout() {
        super.layout()
        highlightView.frame = bounds.insetBy(
            dx: Metrics.highlightHorizontalInset,
            dy: Metrics.highlightVerticalInset
        )
        drawingView.frame = bounds

        let stateImageSize = checkmarkView.image?.size ?? NSSize(
            width: Metrics.stateImageWidth,
            height: Metrics.stateImageHeight
        )
        checkmarkView.frame = NSRect(
            x: titleLeading - Metrics.titleTextInset
                - stateImageSize.width - Metrics.stateImageTitleSpacing,
            y: floor((bounds.height - stateImageSize.height) / 2),
            width: stateImageSize.width,
            height: stateImageSize.height
        )
        submenuIndicatorView.frame = NSRect(
            x: bounds.width - Metrics.trailing - Metrics.submenuIndicatorWidth,
            y: (bounds.height - Metrics.submenuIndicatorHeight) / 2,
            width: Metrics.submenuIndicatorWidth,
            height: Metrics.submenuIndicatorHeight
        )
    }

    var preferredWidth: CGFloat {
        let titleWidth = ceil(
            (label as NSString).size(withAttributes: [.font: titleFont]).width
        )
        let sublabelWidth = sublabelSize.map {
            Metrics.titleBadgeSpacing + $0.width
        } ?? 0
        return min(
            Metrics.maximumWidth,
            max(
                Metrics.minimumWidth,
                titleLeading
                    + titleWidth
                    + sublabelWidth
                    + submenuColumnWidth
                    + Metrics.trailing
            )
        )
    }

    func update(
        label: String,
        sublabel: String?,
        sublabelStyle: TrayMenuItemSublabelStyle,
        checked: Bool,
        keepsMenuOpen: Bool,
        hasSubmenu: Bool
    ) {
        self.label = label
        self.sublabel = sublabel
        self.sublabelStyle = sublabelStyle
        self.checked = checked
        self.keepsMenuOpen = keepsMenuOpen
        self.hasSubmenu = hasSubmenu
        invalidateIntrinsicContentSize()
        refresh()
    }

    func updateMenuItem(
        label: String?,
        sublabel: String?,
        sublabelStyle: TrayMenuItemSublabelStyle?,
        checked: Bool?
    ) {
        if let label {
            self.label = label
        }
        if let sublabel {
            self.sublabel = sublabel
        }
        if let sublabelStyle {
            self.sublabelStyle = sublabelStyle
        }
        if let checked {
            self.checked = checked
        }
        invalidateIntrinsicContentSize()
        refresh()
    }

    override func updateTrackingAreas() {
        if let trackingAreaReference {
            removeTrackingArea(trackingAreaReference)
        }
        let trackingArea = NSTrackingArea(
            rect: .zero,
            options: [.activeAlways, .inVisibleRect, .mouseEnteredAndExited],
            owner: self,
            userInfo: nil
        )
        addTrackingArea(trackingArea)
        trackingAreaReference = trackingArea
        super.updateTrackingAreas()
    }

    override func mouseEntered(with event: NSEvent) {
        if let menu = enclosingMenuItem?.menu as? TrayMenu {
            menu.setHoveredCustomView(self)
        } else {
            setPointerInside(true)
        }
    }

    override func mouseExited(with event: NSEvent) {
        if let menu = enclosingMenuItem?.menu as? TrayMenu {
            menu.clearHoveredCustomView(self)
        } else {
            setPointerInside(false)
        }
    }

    override func mouseDown(with event: NSEvent) {
        refresh()
    }

    override func mouseUp(with event: NSEvent) {
        guard
            let menuItem = enclosingMenuItem,
            menuItem.isEnabled,
            let action = menuItem.action
        else {
            return
        }
        if hasSubmenu {
            return
        }
        if !keepsMenuOpen {
            menuItem.menu?.cancelTracking()
        }
        NSApp.sendAction(action, to: menuItem.target, from: menuItem)
    }

    fileprivate func refresh() {
        needsLayout = true
        drawingView.needsDisplay = true
    }

    fileprivate func setPointerInside(_ inside: Bool) {
        guard pointerInside != inside else {
            return
        }
        pointerInside = inside
        refresh()
    }

    fileprivate var containsSubmenuIndicator: Bool {
        hasSubmenu
    }

    fileprivate func setReservesSubmenuColumn(_ reserves: Bool) {
        guard reservesSubmenuColumn != reserves else {
            return
        }
        reservesSubmenuColumn = reserves
        invalidateIntrinsicContentSize()
        refresh()
    }

    fileprivate func setReservesStateColumn(_ reserves: Bool) {
        guard reservesStateColumn != reserves else {
            return
        }
        reservesStateColumn = reserves
        invalidateIntrinsicContentSize()
        refresh()
    }

    private var titleLeading: CGFloat {
        guard reservesStateColumn else {
            return Metrics.titleLeading + Metrics.titleTextInset
        }
        let stateImageWidth = max(
            Metrics.stateImageWidth,
            checkmarkView.image?.size.width ?? 0
        )
        return Metrics.stateImageLeading + stateImageWidth
            + Metrics.stateImageTitleSpacing + Metrics.titleTextInset
    }

    private func drawContent() {
        guard let menuItem = enclosingMenuItem else {
            return
        }
        let highlighted = menuItem.isEnabled
            && (pointerInside || menuItem.isHighlighted)
        highlightView.isHidden = !highlighted
        let accessoryColor = foregroundColor(
            highlighted: highlighted,
            enabled: menuItem.isEnabled
        )
        checkmarkView.isHidden = !checked
        checkmarkView.contentTintColor = accessoryColor
        submenuIndicatorView.isHidden = !hasSubmenu
        submenuIndicatorView.contentTintColor = accessoryColor
        drawTitle(highlighted: highlighted, enabled: menuItem.isEnabled)
        drawSublabel(highlighted: highlighted, enabled: menuItem.isEnabled)
    }

    private var sublabelSize: NSSize? {
        guard let sublabel, !sublabel.isEmpty else {
            return nil
        }
        let font = sublabelStyle == .secondary ? titleFont : badgeFont
        let textSize = (sublabel as NSString).size(
            withAttributes: [.font: font]
        )
        if sublabelStyle == .secondary {
            return NSSize(
                width: ceil(textSize.width),
                height: ceil(textSize.height)
            )
        }
        return NSSize(
            width: ceil(textSize.width) + Metrics.badgeHorizontalPadding * 2,
            height: Metrics.badgeHeight
        )
    }

    private func foregroundColor(highlighted: Bool, enabled: Bool) -> NSColor {
        if !enabled {
            return .tertiaryLabelColor
        }
        return highlighted ? .selectedMenuItemTextColor : .labelColor
    }

    private func drawTitle(highlighted: Bool, enabled: Bool) {
        let paragraphStyle = NSMutableParagraphStyle()
        paragraphStyle.lineBreakMode = .byTruncatingTail
        let attributes: [NSAttributedString.Key: Any] = [
            .font: titleFont,
            .foregroundColor: foregroundColor(
                highlighted: highlighted,
                enabled: enabled
            ),
            .paragraphStyle: paragraphStyle,
        ]
        let textHeight = ceil(
            (label as NSString).size(withAttributes: attributes).height
        )
        let titleTrailing = sublabelFrame.map {
            $0.minX - Metrics.titleBadgeSpacing
        } ?? (
            bounds.width
                - Metrics.trailing
                - submenuColumnWidth
        )
        let titleRect = NSRect(
            x: titleLeading,
            y: (bounds.height - textHeight) / 2,
            width: max(0, titleTrailing - titleLeading),
            height: textHeight
        )
        (label as NSString).draw(in: titleRect, withAttributes: attributes)
    }

    private var sublabelFrame: NSRect? {
        guard let size = sublabelSize else {
            return nil
        }
        let trailing = Metrics.trailing + submenuColumnWidth
        let maximumWidth = max(
            0,
            bounds.width
                - titleLeading
                - Metrics.minimumTitleWidth
                - Metrics.titleBadgeSpacing
                - trailing
        )
        let width = min(size.width, maximumWidth)
        return NSRect(
            x: bounds.width - trailing - width,
            y: (bounds.height - size.height) / 2,
            width: width,
            height: size.height
        )
    }

    private var submenuColumnWidth: CGFloat {
        guard reservesSubmenuColumn else {
            return 0
        }
        return Metrics.submenuIndicatorWidth + Metrics.submenuColumnSpacing
    }

    private func drawSublabel(highlighted: Bool, enabled: Bool) {
        guard let sublabel, let frame = sublabelFrame else {
            return
        }
        if sublabelStyle == .secondary {
            let color: NSColor
            if !enabled {
                color = .tertiaryLabelColor
            } else if highlighted {
                color = .selectedMenuItemTextColor
            } else {
                color = .secondaryLabelColor
            }
            let paragraphStyle = NSMutableParagraphStyle()
            paragraphStyle.alignment = .right
            paragraphStyle.lineBreakMode = .byTruncatingTail
            (sublabel as NSString).draw(
                in: frame,
                withAttributes: [
                    .font: titleFont,
                    .foregroundColor: color,
                    .paragraphStyle: paragraphStyle,
                ]
            )
            return
        }
        let backgroundColor: NSColor
        let foregroundColor: NSColor
        switch sublabelStyle {
        case .badge:
            backgroundColor = NSColor(
                calibratedRed: 0.20,
                green: 0.80,
                blue: 0.04,
                alpha: enabled ? 1 : 0.45
            )
            foregroundColor = .white
        case .muted:
            backgroundColor = NSColor.secondaryLabelColor.withAlphaComponent(
                enabled ? 0.18 : 0.10
            )
            foregroundColor = enabled ? .secondaryLabelColor : .tertiaryLabelColor
        case .destructive:
            backgroundColor = NSColor.systemRed.withAlphaComponent(
                enabled ? 1 : 0.45
            )
            foregroundColor = .white
        case .warning:
            backgroundColor = NSColor.systemOrange.withAlphaComponent(
                enabled ? 1 : 0.45
            )
            foregroundColor = .white
        case .secondary:
            return
        }
        backgroundColor.setFill()
        NSBezierPath(
            roundedRect: frame,
            xRadius: 4,
            yRadius: 4
        ).fill()
        let text = sublabel as NSString
        let attributes: [NSAttributedString.Key: Any] = [
            .font: badgeFont,
            .foregroundColor: foregroundColor,
        ]
        let textSize = text.size(withAttributes: attributes)
        text.draw(
            at: NSPoint(
                x: frame.midX - textSize.width / 2,
                y: frame.midY - textSize.height / 2
            ),
            withAttributes: attributes
        )
    }

}

final class TrayMenu: NSMenu, NSMenuDelegate {
    private let onSelect: (Int) -> Void
    private weak var hoveredCustomView: TrayMenuItemView?
    private var deferredItems: [[String: Any]]?

    init(
        items: [[String: Any]],
        deferItems: Bool = false,
        onSelect: @escaping (Int) -> Void
    ) {
        self.onSelect = onSelect
        super.init(title: "")
        autoenablesItems = false
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(menuTrackingDidChange(_:)),
            name: NSMenu.didBeginTrackingNotification,
            object: self
        )
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(menuTrackingDidChange(_:)),
            name: NSMenu.didEndTrackingNotification,
            object: self
        )
        if deferItems {
            deferredItems = items
            delegate = self
        } else {
            populate(items)
        }
    }

    private func populate(_ entries: [[String: Any]]) {
        var customViews: [TrayMenuItemView] = []
        for entry in entries {
            let item = makeItem(entry)
            addItem(item)
            if let view = item.view as? TrayMenuItemView {
                customViews.append(view)
            }
        }
        updateCustomViewWidths(customViews)
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        guard let entries = deferredItems else {
            return
        }
        deferredItems = nil
        populate(entries)
    }

    required init(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    deinit {
        NotificationCenter.default.removeObserver(self)
    }

    fileprivate func setHoveredCustomView(_ view: TrayMenuItemView) {
        guard hoveredCustomView !== view else {
            return
        }
        let previousView = hoveredCustomView
        hoveredCustomView = view
        previousView?.setPointerInside(false)
        view.setPointerInside(true)
    }

    fileprivate func clearHoveredCustomView(_ view: TrayMenuItemView) {
        guard hoveredCustomView === view else {
            return
        }
        hoveredCustomView = nil
        view.setPointerInside(false)
    }

    @objc private func menuTrackingDidChange(_ notification: Notification) {
        let previousView = hoveredCustomView
        hoveredCustomView = nil
        previousView?.setPointerInside(false)
    }

    @discardableResult
    func update(items entries: [[String: Any]]) -> Bool {
        guard isCompatible(with: entries) else {
            return false
        }
        apply(entries)
        return true
    }

    @discardableResult
    func updateMenuItems(_ updates: [[String: Any]]) -> Bool {
        var pending: [String: [String: Any]] = [:]
        for arguments in updates {
            guard let key = arguments["key"] as? String else {
                return false
            }
            pending[key, default: [:]].merge(arguments) { _, new in new }
        }
        if pending.isEmpty {
            return true
        }
        var missing = Set(pending.keys)
        collectMatchingKeys(&missing)
        guard missing.isEmpty else {
            return false
        }
        applyMenuItemUpdates(&pending)
        return true
    }

    private func collectMatchingKeys(_ missing: inout Set<String>) {
        if let deferredItems {
            Self.collectMatchingKeys(in: deferredItems, missing: &missing)
            return
        }
        for item in items {
            if missing.isEmpty { return }
            if let key = item.representedObject as? String {
                missing.remove(key)
            }
            (item.submenu as? TrayMenu)?.collectMatchingKeys(&missing)
        }
    }

    private static func collectMatchingKeys(
        in entries: [[String: Any]],
        missing: inout Set<String>
    ) {
        for entry in entries {
            if missing.isEmpty { return }
            if let key = entry["key"] as? String {
                missing.remove(key)
            }
            if let children = entry["items"] as? [[String: Any]] {
                collectMatchingKeys(in: children, missing: &missing)
            }
        }
    }

    private static func applyDeferredUpdates(
        _ pending: inout [String: [String: Any]],
        entries: inout [[String: Any]]
    ) {
        for index in entries.indices {
            if pending.isEmpty { return }
            if let key = entries[index]["key"] as? String,
               let arguments = pending.removeValue(forKey: key) {
                entries[index].merge(arguments) { _, new in new }
            }
            if var children = entries[index]["items"] as? [[String: Any]] {
                applyDeferredUpdates(&pending, entries: &children)
                entries[index]["items"] = children
            }
        }
    }

    private func applyMenuItemUpdates(_ pending: inout [String: [String: Any]]) {
        if var entries = deferredItems {
            deferredItems = nil
            Self.applyDeferredUpdates(&pending, entries: &entries)
            deferredItems = entries
            return
        }
        var changed = false
        for item in items {
            if pending.isEmpty { break }
            if let key = item.representedObject as? String,
               let arguments = pending.removeValue(forKey: key) {
                applyMenuItemUpdate(arguments, to: item)
                changed = true
            }
            (item.submenu as? TrayMenu)?.applyMenuItemUpdates(&pending)
        }
        if changed {
            updateCustomViewWidths(items.compactMap { $0.view as? TrayMenuItemView })
        }
    }

    private func applyMenuItemUpdate(_ arguments: [String: Any], to item: NSMenuItem) {
        let label = arguments["label"] as? String
        let sublabel = arguments["sublabel"] as? String
        let style = (arguments["sublabelStyle"] as? String).flatMap(
            TrayMenuItemSublabelStyle.init(rawValue:)
        )
        let nativeItem = item as? TrayNativeMenuItem
        let checked = nativeItem?.trayType == .checkbox
            ? arguments["checked"] as? Bool
            : nil
        if let label {
            item.title = label
        }
        if let enabled = arguments["enabled"] as? Bool {
            item.isEnabled = enabled
            if nativeItem?.trayType != .submenu {
                item.action = enabled ? #selector(didSelectItem(_:)) : nil
            }
        }
        if let checked {
            item.state = checked ? .on : .off
        }
        if let view = item.view as? TrayMenuItemView {
            view.updateMenuItem(
                label: label,
                sublabel: sublabel,
                sublabelStyle: style,
                checked: checked
            )
        } else if let sublabel, !sublabel.isEmpty,
                  let type = nativeItem?.trayType {
            item.view = TrayMenuItemView(
                label: item.title,
                sublabel: sublabel,
                sublabelStyle: style
                    ?? (type == .submenu ? .secondary : .badge),
                checked: type == .checkbox && item.state == .on,
                keepsMenuOpen: false,
                hasSubmenu: type == .submenu
            )
        }
    }

    private func makeItem(_ entry: [String: Any]) -> NSMenuItem {
        guard let typeName = entry["type"] as? String,
              let type = TrayMenuItemType(rawValue: typeName) else {
            return NSMenuItem.separator()
        }
        if type == .separator {
            return NSMenuItem.separator()
        }
        let item = TrayNativeMenuItem(type: type)
        item.title = entry["label"] as? String ?? ""
        item.tag = entry["id"] as? Int ?? 0
        item.representedObject = entry["key"] as? String
        item.isEnabled = entry["enabled"] as? Bool ?? true
        applyKeyboardShortcut(entry, to: item)
        switch type {
        case .checkbox:
            item.state = (entry["checked"] as? Bool ?? false) ? .on : .off
            item.target = self
            item.action = item.isEnabled ? #selector(didSelectItem(_:)) : nil
        case .submenu:
            let children = entry["items"] as? [[String: Any]] ?? []
            setSubmenu(
                TrayMenu(items: children, deferItems: true, onSelect: onSelect),
                for: item
            )
        case .action:
            item.target = self
            item.action = item.isEnabled ? #selector(didSelectItem(_:)) : nil
        case .separator:
            break
        }
        let sublabel = entry["sublabel"] as? String
        let keepsMenuOpen = entry["keepsMenuOpen"] as? Bool ?? false
        let usesCustomView = entry["usesCustomView"] as? Bool ?? false
        if usesCustomView || sublabel?.isEmpty == false || keepsMenuOpen {
            let styleName = entry["sublabelStyle"] as? String ?? "badge"
            item.view = TrayMenuItemView(
                label: item.title,
                sublabel: sublabel,
                sublabelStyle: TrayMenuItemSublabelStyle(
                    rawValue: styleName
                ) ?? .badge,
                checked: item.state == .on,
                keepsMenuOpen: keepsMenuOpen,
                hasSubmenu: type == .submenu
            )
        }
        return item
    }

    private func isCompatible(with entries: [[String: Any]]) -> Bool {
        if deferredItems != nil {
            return true
        }
        guard entries.count == items.count else {
            return false
        }
        for (entry, item) in zip(entries, items) {
            guard let typeName = entry["type"] as? String,
                  let type = TrayMenuItemType(rawValue: typeName) else {
                return false
            }
            if type == .separator {
                guard item.isSeparatorItem else {
                    return false
                }
                continue
            }
            guard let nativeItem = item as? TrayNativeMenuItem,
                  nativeItem.trayType == type,
                  !item.isSeparatorItem else {
                return false
            }
            if type == .submenu {
                guard let children = entry["items"] as? [[String: Any]],
                      let submenu = item.submenu as? TrayMenu,
                      submenu.isCompatible(with: children) else {
                    return false
                }
            } else if item.submenu != nil {
                return false
            }
        }
        return true
    }

    private func apply(_ entries: [[String: Any]]) {
        if deferredItems != nil {
            deferredItems = entries
            return
        }
        var customViews: [TrayMenuItemView] = []
        for (entry, item) in zip(entries, items) {
            guard let typeName = entry["type"] as? String,
                  let type = TrayMenuItemType(rawValue: typeName),
                  type != .separator else {
                continue
            }
            let label = entry["label"] as? String ?? ""
            let sublabel = entry["sublabel"] as? String
            let styleName = entry["sublabelStyle"] as? String ?? "badge"
            let style = TrayMenuItemSublabelStyle(rawValue: styleName) ?? .badge
            let checked = entry["checked"] as? Bool ?? false
            let keepsMenuOpen = entry["keepsMenuOpen"] as? Bool ?? false
            let usesCustomView = entry["usesCustomView"] as? Bool ?? false

            item.title = label
            item.tag = entry["id"] as? Int ?? 0
            item.representedObject = entry["key"] as? String
            item.isEnabled = entry["enabled"] as? Bool ?? true
            item.target = type == .submenu ? nil : self
            item.action = type != .submenu && item.isEnabled
                ? #selector(didSelectItem(_:))
                : nil
            item.state = type == .checkbox && checked ? .on : .off
            applyKeyboardShortcut(entry, to: item)

            if type == .submenu,
               let children = entry["items"] as? [[String: Any]],
               let submenu = item.submenu as? TrayMenu {
                submenu.apply(children)
            }

            if usesCustomView || sublabel?.isEmpty == false || keepsMenuOpen {
                let view: TrayMenuItemView
                if let currentView = item.view as? TrayMenuItemView {
                    view = currentView
                    view.update(
                        label: label,
                        sublabel: sublabel,
                        sublabelStyle: style,
                        checked: checked,
                        keepsMenuOpen: keepsMenuOpen,
                        hasSubmenu: type == .submenu
                    )
                } else {
                    view = TrayMenuItemView(
                        label: label,
                        sublabel: sublabel,
                        sublabelStyle: style,
                        checked: checked,
                        keepsMenuOpen: keepsMenuOpen,
                        hasSubmenu: type == .submenu
                    )
                    item.view = view
                }
                customViews.append(view)
            } else {
                item.view = nil
            }
        }
        updateCustomViewWidths(customViews)
    }

    private func applyKeyboardShortcut(
        _ entry: [String: Any],
        to item: NSMenuItem
    ) {
        item.keyEquivalent = ""
        item.keyEquivalentModifierMask = []
        guard let keyEquivalent = entry["keyEquivalent"] as? String else {
            return
        }
        item.keyEquivalent = keyEquivalent
        var result: NSEvent.ModifierFlags = []
        let modifiers = entry["keyEquivalentModifiers"] as? [String] ?? []
        for modifier in modifiers {
            switch modifier {
            case "option":
                result.insert(.option)
            case "capsLock":
                result.insert(.capsLock)
            case "control":
                result.insert(.control)
            case "function":
                result.insert(.function)
            case "command":
                result.insert(.command)
            case "shift":
                result.insert(.shift)
            default:
                break
            }
        }
        item.keyEquivalentModifierMask = result
    }

    private func updateCustomViewWidths(_ customViews: [TrayMenuItemView]) {
        let reservesStateColumn = items.contains { item in
            guard !item.isHidden && !item.isSeparatorItem else {
                return false
            }
            switch item.state {
            case .on:
                return item.onStateImage != nil
            case .mixed:
                return item.mixedStateImage != nil
            default:
                return item.offStateImage != nil
            }
        }
        let reservesSubmenuColumn = customViews.contains {
            $0.containsSubmenuIndicator
        }
        for view in customViews {
            view.setReservesStateColumn(reservesStateColumn)
            view.setReservesSubmenuColumn(reservesSubmenuColumn)
        }
        let width = customViews.map(\.preferredWidth).max() ?? 0
        for view in customViews {
            view.frame.size.width = width
            view.refresh()
        }
    }

    @objc private func didSelectItem(_ sender: NSMenuItem) {
        onSelect(sender.tag)
    }
}
