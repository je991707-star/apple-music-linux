// Which theme mode is in effect. Until an Apple Music account is signed in the plain blur (glass)
// look is used and no accent palette CSS is applied: the Settings panel where the theme is chosen
// only exists once signed in, so a signed-out user could not otherwise change the colouring.
//
//   blur    no theme CSS; the glass look from the base styles
//   accent  the saved colour palette (the Apple Music preset unless the user changed it)
//   custom  the user's own CSS file
export function resolveThemeMode({ signedIn, themeMode, isHyprland }) {
    if (!signedIn) return 'blur';
    // Blur works on every desktop (the wallpaper is drawn blurred behind the interface), so a saved
    // choice is always honoured. With none saved, Hyprland defaults to blur and the rest to accent.
    return themeMode || (isHyprland ? 'blur' : 'accent');
}
