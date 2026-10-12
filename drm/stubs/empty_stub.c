/*
 * Stand-in for Android system libraries (libandroid.so, libOpenSLES.so) that
 * Apple's libraries list as dependencies but never call into. Shipping the real
 * ones would pull in the whole Android graphics/media stack.
 */
char aml_empty_stub;
