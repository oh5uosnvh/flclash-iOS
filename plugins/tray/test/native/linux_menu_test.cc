#include "../../linux/tray_plugin.cc"
#include <iostream>
#include <libdbusmenu-gtk/parser.h>
#include <stdexcept>
#include <string>

static void check(bool condition, const char *message) {
  if (!condition)
    throw std::runtime_error(message);
}

static FlValue *node(int id) {
  FlValue *entry = fl_value_new_map();
  fl_value_set_string_take(entry, "type", fl_value_new_string("checkbox"));
  fl_value_set_string_take(entry, "id", fl_value_new_int(id));
  fl_value_set_string_take(
      entry, "key",
      fl_value_new_string(("node-" + std::to_string(id)).c_str()));
  fl_value_set_string_take(entry, "label", fl_value_new_string("香港 & A"));
  fl_value_set_string_take(entry, "sublabel", fl_value_new_string("42 ms"));
  fl_value_set_string_take(entry, "checked", fl_value_new_bool(false));
  return entry;
}

static FlValue *group(const char *name, FlValue *items) {
  FlValue *entry = fl_value_new_map();
  fl_value_set_string_take(entry, "type", fl_value_new_string("submenu"));
  fl_value_set_string_take(entry, "label", fl_value_new_string(name));
  fl_value_set_string_take(entry, "items", items);
  return entry;
}

static GtkWidget *child_at(GtkWidget *menu, guint position) {
  GList *children = gtk_container_get_children(GTK_CONTAINER(menu));
  GtkWidget *child = GTK_WIDGET(g_list_nth_data(children, position));
  g_list_free(children);
  return child;
}

static guint child_count(GtkWidget *menu) {
  GList *children = gtk_container_get_children(GTK_CONTAINER(menu));
  guint length = g_list_length(children);
  g_list_free(children);
  return length;
}

static void drain_events() {
  while (g_main_context_iteration(nullptr, FALSE)) {
  }
}

int main(int argc, char **argv) {
  gtk_init(&argc, &argv);
  try {
    g_autoptr(FlValue) items = fl_value_new_list();
    FlValue *nodes = fl_value_new_list();
    for (int i = 0; i < 2000; ++i)
      fl_value_append_take(nodes, node(1024 + i));
    fl_value_append_take(items, group("Proxy", nodes));
    FlValue *nested = fl_value_new_list();
    fl_value_append_take(nested, node(5000));
    FlValue *other = fl_value_new_list();
    fl_value_append_take(other, group("Nested", nested));
    fl_value_append_take(items, group("Other", other));
    GtkWidget *menu = build_menu(items);
    g_object_ref_sink(menu);
    g_clear_pointer(&items, fl_value_unref);
    gtk_widget_show_all(menu);
    DbusmenuMenuitem *exported = dbusmenu_gtk_parse_menu_structure(menu);
    drain_events();

    TrayPlugin plugin{};
    plugin.menu = menu;
    GtkWidget *first_item = child_at(menu, 0);
    GtkWidget *first = gtk_menu_item_get_submenu(GTK_MENU_ITEM(first_item));
    GtkWidget *other_item = child_at(menu, 1);
    GtkWidget *other_menu =
        gtk_menu_item_get_submenu(GTK_MENU_ITEM(other_item));
    check(child_count(menu) == 2, "root menu missing groups");
    check(child_count(first) == 2000 && child_count(other_menu) == 1,
          "initial menu is missing submenu rows");
    GtkWidget *selected = find_menu_item(menu, "node-1024");
    check(selected != nullptr, "unopened node has no native widget");
    GtkWidget *nested_item = child_at(other_menu, 0);
    GtkWidget *nested_menu =
        gtk_menu_item_get_submenu(GTK_MENU_ITEM(nested_item));
    check(child_count(nested_menu) == 1 &&
              find_menu_item(menu, "node-5000") != nullptr,
          "initial menu is missing nested submenu rows");
    auto *first_exported = dbusmenu_gtk_parse_get_cached_item(first_item);
    check(g_strcmp0(dbusmenu_menuitem_property_get(
                        first_exported, DBUSMENU_MENUITEM_PROP_CHILD_DISPLAY),
                    "submenu") == 0,
          "group is not exported as a submenu");
    check(g_list_length(dbusmenu_menuitem_get_children(first_exported)) == 2000,
          "initial export is missing submenu rows");
    auto *nested_exported = dbusmenu_gtk_parse_get_cached_item(nested_item);
    check(g_list_length(dbusmenu_menuitem_get_children(nested_exported)) == 1,
          "initial export is missing nested submenu rows");

    g_autoptr(FlValue) update = fl_value_new_map();
    fl_value_set_string_take(update, "key", fl_value_new_string("node-1024"));
    fl_value_set_string_take(update, "sublabel",
                             fl_value_new_string("Timeout"));
    fl_value_set_string_take(update, "checked", fl_value_new_bool(true));
    check(apply_menu_item_update(&plugin, update), "unopened update rejected");

    g_autoptr(FlValue) bad_batch = fl_value_new_map();
    FlValue *updates = fl_value_new_list();
    FlValue *changed = fl_value_new_map();
    fl_value_set_string_take(changed, "key", fl_value_new_string("node-1024"));
    fl_value_set_string_take(changed, "label", fl_value_new_string("wrong"));
    fl_value_append_take(updates, changed);
    FlValue *missing = fl_value_new_map();
    fl_value_set_string_take(missing, "key", fl_value_new_string("missing"));
    fl_value_append_take(updates, missing);
    fl_value_set_string_take(bad_batch, "updates", updates);
    g_autoptr(FlMethodResponse) response =
        handle_update_menu_items(&plugin, bad_batch);
    check(!fl_value_get_bool(fl_method_success_response_get_result(
              FL_METHOD_SUCCESS_RESPONSE(response))),
          "invalid batch was accepted");

    check(g_strcmp0(gtk_menu_item_get_label(GTK_MENU_ITEM(selected)),
                    "香港 & A  (Timeout)") == 0,
          "unopened update lost or invalid batch partially applied");
    check(gtk_check_menu_item_get_active(GTK_CHECK_MENU_ITEM(selected)),
          "unopened checkmark update lost");
    dbusmenu_menuitem_send_about_to_show(first_exported, nullptr, nullptr);
    drain_events();
    check(child_count(first) == 2000 &&
              find_menu_item(menu, "node-1024") == selected,
          "opening rebuilt submenu rows");
    check(dbusmenu_gtk_parse_get_cached_item(first_item) == first_exported &&
              g_list_length(dbusmenu_menuitem_get_children(first_exported)) ==
                  2000,
          "opening changed the exported submenu");
    fl_value_set_string_take(update, "sublabel", fl_value_new_string("17 ms"));
    check(apply_menu_item_update(&plugin, update), "visible update rejected");
    check(g_strcmp0(gtk_menu_item_get_label(GTK_MENU_ITEM(selected)),
                    "香港 & A  (17 ms)") == 0,
          "visible update lost");

    fl_value_set_string_take(update, "key", fl_value_new_string("node-5000"));
    check(apply_menu_item_update(&plugin, update), "nested update rejected");
    check(g_strcmp0(
              gtk_menu_item_get_label(GTK_MENU_ITEM(child_at(nested_menu, 0))),
              "香港 & A  (17 ms)") == 0,
          "nested update lost");
    gtk_widget_destroy(menu);
    g_object_unref(menu);
    g_object_unref(exported);
    drain_events();
    plugin.menu = build_menu(nullptr);
    g_object_ref_sink(plugin.menu);
    check(!apply_menu_item_update(&plugin, update),
          "stale key survived rebuild");
    release_tray(&plugin);
    std::cout << "Native Linux menu tests passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
